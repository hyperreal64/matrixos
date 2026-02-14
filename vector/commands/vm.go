package commands

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	qemuSystemX86_64 = "qemu-system-x86_64"
)

type VMDriver struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	reader *bufio.Reader
}

func NewVMDriver(args []string) (*VMDriver, error) {
	cmd := exec.Command(qemuSystemX86_64, args...)
	// cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start qemu: %v", err)
	}

	return &VMDriver{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		reader: bufio.NewReader(stdout),
	}, nil
}

// Expect waits for a specific string in the output within a timeout
func (vm *VMDriver) Expect(target string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Compile regex to strip specific terminal control sequences that pollute output
	// Matches CSI sequences (excluding SGR/colors 'm') and Device Control Strings
	ansiStrip := regexp.MustCompile(`\x1b\[[0-9;?]*[a-ln-zA-Z]|\x1bP.*?\x1b\\`)

	resultCh := make(chan error)

	go func() {
		buf := make([]byte, 1024)
		var matchBuf string
		for {
			n, err := vm.reader.Read(buf)
			if n > 0 {
				chunk := string(buf[:n])
				fmt.Print(ansiStrip.ReplaceAllString(chunk, ""))
				matchBuf += chunk
				if strings.Contains(matchBuf, target) {
					resultCh <- nil
					return
				}
				// Prevent unbounded growth of the match buffer
				// Keep enough context for the target string
				if len(matchBuf) > 4096 {
					matchBuf = matchBuf[len(matchBuf)-2048:]
				}
			}
			if err != nil {
				if err != io.EOF {
					resultCh <- err
				} else {
					// EOF reached but target not found yet, loop will exit next read
					resultCh <- fmt.Errorf("EOF reached while waiting for pattern: %q", target)
				}
				return
			}
		}
	}()

	select {
	case err := <-resultCh:
		return err
	case <-ctx.Done():
		return fmt.Errorf("timeout waiting for pattern: %q", target)
	}
}

// Send writes a command to the VM
func (vm *VMDriver) Send(cmd string) error {
	_, err := fmt.Fprintf(vm.stdin, "%s\n", cmd)
	return err
}

func (vm *VMDriver) Close() {
	if vm.cmd.Process != nil {
		vm.cmd.Process.Kill()
	}
}

// Wait waits for the VM process to exit
func (vm *VMDriver) Wait() error {
	return vm.cmd.Wait()
}

// VMCommand checks matrixOS images via QEMU
type VMCommand struct {
	fs          *flag.FlagSet
	imagePath   string
	memory      string
	port        string
	waitBoot    int
	nographic   bool
	noAudio     bool
	interactive bool
	cpus        string
}

// NewVMCommand creates a new VMCommand
func NewVMCommand() ICommand {
	c := &VMCommand{
		fs: flag.NewFlagSet("vm", flag.ExitOnError),
	}
	c.fs.StringVar(&c.imagePath, "image", "", "Path to the matrixOS image")
	c.fs.StringVar(&c.memory, "memory", "8G", "Amount of RAM for the VM")
	c.fs.StringVar(&c.port, "port", "2222", "Local port for SSH forwarding")
	c.fs.IntVar(&c.waitBoot, "wait_boot", 300, "Seconds to wait for boot login prompt")
	c.fs.BoolVar(&c.nographic, "nographic", false, "Disable graphical output")
	c.fs.BoolVar(&c.noAudio, "noaudio", false, "Disable audio devices")
	c.fs.BoolVar(&c.interactive, "interactive", false, "Run VM interactively without testing")
	c.fs.StringVar(&c.cpus, "cpus", "4", "Number of CPUs for the VM")
	return c
}

// Name returns the name of the command
func (c *VMCommand) Name() string {
	return c.fs.Name()
}

// Init initializes the command
func (c *VMCommand) Init(args []string) error {
	c.fs.Usage = func() {
		fmt.Printf("Usage: vector dev %s\n", c.Name())
		c.fs.PrintDefaults()
	}
	if err := c.fs.Parse(args); err != nil {
		return err
	}
	return nil
}

// Run runs the command
func (c *VMCommand) Run() error {
	if c.imagePath == "" {
		c.fs.Usage()
		return fmt.Errorf("missing required flag: --image")
	}

	if !strings.Contains(c.imagePath, "amd64") {
		return fmt.Errorf("only amd64 images are supported (image path must contain 'amd64')")
	}

	tempDir, err := os.MkdirTemp("", "matrixos-vm-")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	varsDst := filepath.Join(tempDir, "my_vars.fd")
	if err := copyFile("/usr/share/edk2-ovmf/OVMF_VARS.fd", varsDst); err != nil {
		return fmt.Errorf("failed to copy OVMF_VARS.fd: %w", err)
	}

	format := "raw"
	if strings.HasSuffix(c.imagePath, ".qcow2") {
		format = "qcow2"
	}

	qemuArgs := []string{
		"-enable-kvm", "-m", c.memory,
		"-cpu", "host", "-smp", c.cpus,
		"-nic", fmt.Sprintf("user,model=virtio-net-pci,hostfwd=tcp::%s-:22", c.port),
		"-drive", "if=pflash,format=raw,readonly=on,file=/usr/share/edk2-ovmf/OVMF_CODE.fd",
		"-drive", "if=pflash,format=raw,file=" + varsDst,
		"-drive", fmt.Sprintf("file=%s,format=%s,if=virtio", c.imagePath, format),
	}

	if c.nographic {
		qemuArgs = append(qemuArgs, "-nographic")
	} else {
		qemuArgs = append(qemuArgs,
			"-serial", "stdio",
			"-device", "virtio-vga-gl,hostmem=512M,blob=true,venus=on",
			"-display", "gtk,gl=on",
		)
	}

	if !c.interactive {
		// Inject a custom SMBIOS serial number to trigger serial console in GRUB
		// See image/boot/*/*/*/grub.cfg
		qemuArgs = append(qemuArgs, "-smbios", "type=1,serial=matrixos-testmode=serial")
	}

	if !c.noAudio {
		qemuArgs = append(qemuArgs,
			"-audiodev", "pa,id=snd0",
			"-device", "intel-hda",
			"-device", "hda-duplex,audiodev=snd0",
		)
	}

	log.Printf("QEMU args: %v", qemuArgs)

	vm, err := NewVMDriver(qemuArgs)
	if err != nil {
		return fmt.Errorf("failed to init VM: %w", err)
	}
	defer vm.Close()

	if c.interactive {
		log.Println("Starting VM in interactive mode...")
		if err := vm.Wait(); err != nil {
			return fmt.Errorf("VM exited with error: %w", err)
		}
		return nil
	}

	log.Println("Starting matrixOS VM Test...")
	return c.runTests(vm)
}

func (c *VMCommand) runTests(vm *VMDriver) error {
	log.Println("Waiting for login prompt...")
	waitBoot := time.Duration(c.waitBoot) * time.Second
	if err := vm.Expect("matrixos login:", waitBoot); err != nil {
		return fmt.Errorf("boot failed: %w", err)
	}

	log.Println("Logging in...")
	if err := vm.Send("root"); err != nil {
		return err
	}
	if err := vm.Expect("Password:", 5*time.Second); err != nil {
		return fmt.Errorf("password prompt missing: %w", err)
	}
	if err := vm.Send("matrix"); err != nil {
		return err
	}

	if err := vm.Expect("#", 10*time.Second); err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	log.Println("Checking OS Release...")
	if err := vm.Send("grep ID=matrixos /etc/os-release"); err != nil {
		return err
	}
	if err := vm.Expect("ID=matrixos", 5*time.Second); err != nil {
		return fmt.Errorf("OS check failed: %w", err)
	}

	var lastErr error
	start := time.Now()
	end := start.Add(30 * time.Second)
	for ; time.Now().Before(end); time.Sleep(2 * time.Second) {
		// Ensure resolv.conf is healthy.
		log.Println("Checking resolv.conf...")
		if err := vm.Send("grep nameserver /etc/resolv.conf"); err != nil {
			lastErr = err
			log.Printf("Failed to send resolv.conf check command, retrying... : %v\n", err)
			continue
		}
		if err := vm.Expect("nameserver", 5*time.Second); err != nil {
			lastErr = err
			log.Printf("Failed to send resolv.conf check command, retrying... : %v\n", err)
			continue
		}
		lastErr = nil
		break
	}

	if lastErr != nil {
		log.Println("resolv.conf check failed after multiple attempts.")
		return fmt.Errorf("resolv.conf check failed after multiple attempts: %w", lastErr)
	}

	log.Println("resolv.conf looks good.")
	log.Println("SUCCESS: Image verified successfully.")
	return nil
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}
