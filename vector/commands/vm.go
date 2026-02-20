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
	rootPassword     = "matrix"
)

type VMDriver struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	reader *bufio.Reader
}

func NewVMDriver(ctx context.Context, args []string) (*VMDriver, error) {
	cmd := exec.CommandContext(ctx, qemuSystemX86_64, args...)
	cmd.Stderr = os.Stderr

	cmd.Cancel = func() error {
		// Do not send SIGKILL immediately when canceling ctx.
		return cmd.Process.Signal(os.Interrupt)
	}
	// Wait 30s before sending SIGKILL.
	cmd.WaitDelay = 30 * time.Second

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	return &VMDriver{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		reader: bufio.NewReader(stdout),
	}, nil
}

// Start starts the VM process
func (vm *VMDriver) Start() error {
	return vm.cmd.Start()
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

func (vm *VMDriver) Close() error {
	return vm.Wait()
}

// Wait waits for the VM process to exit
func (vm *VMDriver) Wait() error {
	return vm.cmd.Wait()
}

// VMCommand checks matrixOS images via QEMU
type VMCommand struct {
	BaseCommand
	fs          *flag.FlagSet
	imagePath   string
	memory      string
	port        string
	waitBoot    time.Duration
	waitTests   time.Duration
	maxRunTime  time.Duration
	nographic   bool
	noAudio     bool
	interactive bool
	audioDev    string
	cpus        string
}

// NewVMCommand creates a new VMCommand
func NewVMCommand() ICommand {
	c := &VMCommand{
		fs: flag.NewFlagSet("vm", flag.ExitOnError),
	}
	c.fs.StringVar(&c.imagePath, "image", "", "Path to the matrixOS image")
	c.fs.StringVar(&c.memory, "memory", "4G", "Amount of RAM for the VM")
	c.fs.StringVar(&c.audioDev, "audio_dev", "pipewire", "Audio device for the VM (default 'pipewire' for PipeWire)")
	c.fs.StringVar(&c.port, "port", "2222", "Local port for SSH forwarding")
	c.fs.DurationVar(&c.waitBoot, "wait_boot", 120*time.Second, "Seconds to wait for boot login prompt")
	c.fs.DurationVar(&c.waitTests, "wait_tests", 120*time.Second, "Seconds to wait for tests to complete")
	c.fs.DurationVar(&c.maxRunTime, "max_run_time", 300*time.Second, "Maximum seconds to allow the entire VM run (including boot and tests), when running in non-interactive mode")
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
	if err := c.initBaseConfig(); err != nil {
		return err
	}
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

	mroot, err := c.cfg.GetItem("matrixOS.Root")
	if err != nil {
		return fmt.Errorf("failed to get matrixOS.Root from config: %w", err)
	}

	codeSrc := filepath.Join(mroot, "vector/tests/data/OVMF_CODE.fd")
	codeDst := filepath.Join(tempDir, "OVMF_CODE.fd")
	if err := copyFile(codeSrc, codeDst); err != nil {
		return fmt.Errorf("failed to copy OVMF_CODE.fd: %w", err)
	}

	varsSrc := filepath.Join(mroot, "vector/tests/data/OVMF_VARS.fd")
	varsDst := filepath.Join(tempDir, "OVMF_VARS.fd")
	if err := copyFile(varsSrc, varsDst); err != nil {
		return fmt.Errorf("failed to copy OVMF_VARS.fd: %w", err)
	}

	// Generate an ext4 filesystem from vector/tests/vm-suite to inject into the VM for testing.
	// This allows us to easily add test scripts and data without modifying the image build process.
	testImageFile, err := os.CreateTemp("", "vm-suite-*.img")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(testImageFile.Name())
	truncCmd := exec.Command("truncate", "-s", "64M", testImageFile.Name())
	if err := truncCmd.Run(); err != nil {
		return fmt.Errorf("failed to truncate test image: %w", err)
	}
	testImageFile.Close()

	mkfsTestImgArgs := []string{
		"mkfs.ext4",
		"-L", "TESTDATA",
		"-F",
		"-d", "tests/vm-suite",
		testImageFile.Name(),
	}
	log.Printf("Generating test filesystem with command: %v\n", mkfsTestImgArgs)
	if err := exec.Command(mkfsTestImgArgs[0], mkfsTestImgArgs[1:]...).Run(); err != nil {
		return fmt.Errorf("failed to create test image filesystem: %w", err)
	}

	format := "raw"
	if strings.HasSuffix(c.imagePath, ".qcow2") {
		format = "qcow2"
	}

	qemuArgs := []string{
		"-enable-kvm", "-m", c.memory,
		"-cpu", "host", "-smp", c.cpus,
		"-nic", fmt.Sprintf("user,model=virtio-net-pci,hostfwd=tcp::%s-:22", c.port),
		"-drive", "if=pflash,format=raw,readonly=on,file=" + codeDst,
		"-drive", "if=pflash,format=raw,file=" + varsDst,
		"-drive", fmt.Sprintf("file=%s,format=%s,if=virtio", c.imagePath, format),
		"-drive", fmt.Sprintf("file=%s,format=raw,if=virtio", testImageFile.Name()),
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
			"-audiodev", fmt.Sprintf("%s,id=snd0", c.audioDev),
			"-device", "intel-hda",
			"-device", "hda-duplex,audiodev=snd0",
		)
	}

	log.Printf("QEMU args: %v", qemuArgs)
	if c.interactive {
		return c.runInteractive(qemuArgs)
	} else {
		return c.runTests(qemuArgs)
	}
}

func (c *VMCommand) runInteractive(qemuArgs []string) error {
	log.Println("Starting VM in interactive mode...")
	vm, err := NewVMDriver(context.Background(), qemuArgs)
	if err != nil {
		return fmt.Errorf("failed to init VM: %w", err)
	}
	defer vm.Close()

	if err := vm.Start(); err != nil {
		return fmt.Errorf("failed to start VM: %w", err)
	}
	if err := vm.Wait(); err != nil {
		return fmt.Errorf("VM exited with error: %w", err)
	}
	return nil
}

func (c *VMCommand) runTests(qemuArgs []string) error {
	log.Println("Starting VM Test...")
	// How long do we allow the whole test suite to run?
	ctx, cancel := context.WithTimeout(context.Background(), c.maxRunTime)
	defer cancel()

	vm, err := NewVMDriver(ctx, qemuArgs)
	if err != nil {
		return fmt.Errorf("failed to init VM: %w", err)
	}
	defer vm.Close()

	if err := vm.Start(); err != nil {
		return fmt.Errorf("failed to start VM: %w", err)
	}

	log.Println("Waiting for login prompt...")
	if err := vm.Expect("matrixos login:", c.waitBoot); err != nil {
		return fmt.Errorf("boot failed: %w", err)
	}

	log.Println("Logging in...")
	if err := vm.Send("root"); err != nil {
		return err
	}
	if err := vm.Expect("Password:", 5*time.Second); err != nil {
		return fmt.Errorf("password prompt missing: %w", err)
	}
	if err := vm.Send(rootPassword); err != nil {
		return err
	}

	if err := vm.Expect("#", 5*time.Second); err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	log.Println("Starting test suite ...")
	if err := vm.Send(`
mkdir -p /tmp/tests
mount /dev/disk/by-label/TESTDATA /tmp/tests
cd /tmp/tests
./main.sh
echo "TEST_RESULT:${?}"
`); err != nil {
		return err
	}

	startTestsTime := time.Now()

	waitLeft := c.waitTests - time.Since(startTestsTime)
	if waitLeft <= 0 {
		return fmt.Errorf("invalid wait time for tests: %v", waitLeft)
	}
	if err := vm.Expect("TEST_RESULT:0", waitLeft); err != nil {
		return fmt.Errorf("Test suite failed: %w", err)
	}

	waitLeft = c.waitTests - time.Since(startTestsTime)
	if waitLeft <= 0 {
		return fmt.Errorf("no time left to wait for VM shutdown: %v", waitLeft)
	}

	log.Println("Test suite passed, shutting down VM...")
	if err := vm.Send("poweroff"); err != nil {
		return err
	}

	waitLeft = c.waitTests - time.Since(startTestsTime)
	if waitLeft <= 0 {
		return fmt.Errorf("no time left to wait for VM shutdown: %v", waitLeft)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), waitLeft)
	defer shutdownCancel()
	log.Println("Waiting for VM to shutdown...")

	done := make(chan error, 1)
	go func() {
		done <- vm.Wait()
	}()

	select {
	case err := <-done:
		// VM exited voluntarily, check if it was a clean shutdown
		if err != nil {
			return fmt.Errorf("VM shutdown failed: %w", err)
		}
		// fall through to success.
	case <-shutdownCtx.Done():
		log.Println("VM did not shutdown in time, killing process...")
		cancel()

		err = <-done // wait for the kill to complete.
		if err != nil {
			return fmt.Errorf(
				"VM shutdown failed, ctx err: %v: %w",
				ctx.Err(),
				err,
			)
		}
		return fmt.Errorf("VM shutdown timed out and was killed")
	}

	log.Println("SUCCESS: Tests passed.")
	return nil
}
