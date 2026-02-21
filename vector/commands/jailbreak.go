package commands

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"matrixos/vector/lib/cds"
	"matrixos/vector/lib/config"
	fslib "matrixos/vector/lib/filesystems"
)

// mountInfo holds the UUID and filesystem type for a mountpoint.
type mountInfo struct {
	UUID   string
	FSType string
}

// jailbreakRunner abstracts OS-level operations so they can be replaced in tests.
type jailbreakRunner struct {
	// execCommand wraps exec.Command for spawning processes.
	execCommand func(name string, args ...string) cmdRunner
	// readFile reads a file's content.
	readFile func(path string) ([]byte, error)
	// writeFile writes data to a file with the given permissions.
	writeFile func(path string, data []byte, perm os.FileMode) error
	// appendFile appends data to a file.
	appendFile func(path string, data []byte) error
	// mkdirAll creates directories recursively.
	mkdirAll func(path string, perm os.FileMode) error
	// stat returns file info for a path.
	stat func(path string) (os.FileInfo, error)
	// removeFile removes a file.
	removeFile func(path string) error
	// remove recursively removes a path.
	removeAll func(path string) error
	// rename renames a file.
	rename func(src, dst string) error
	// realpath resolves a path to its real absolute form.
	realpath func(path string) (string, error)
	// copyFile copies src to dst.
	copyFile func(src, dst string) error
	// getMountInfo returns UUID and filesystem type for a mountpoint.
	getMountInfo func(mnt string) (*mountInfo, error)
	// remountRW remounts a filesystem read-write.
	remountRW func(mnt string) error
	// stdin provides user input for confirmation prompts.
	stdin io.Reader
	// stdout is the writer for info output.
	stdout io.Writer
	// stderr is the writer for error/warning output.
	stderr io.Writer
}

// cmdRunner abstracts an exec.Cmd for testability.
type cmdRunner interface {
	Run() error
	Output() ([]byte, error)
	SetStdout(w io.Writer)
	SetStderr(w io.Writer)
}

// realCmdRunner wraps a real os/exec.Cmd.
type realCmdRunner struct {
	cmd interface {
		Run() error
		Output() ([]byte, error)
	}
	stdout *io.Writer
	stderr *io.Writer
}

func (r *realCmdRunner) Run() error              { return r.cmd.Run() }
func (r *realCmdRunner) Output() ([]byte, error) { return r.cmd.Output() }
func (r *realCmdRunner) SetStdout(w io.Writer)   { *r.stdout = w }
func (r *realCmdRunner) SetStderr(w io.Writer)   { *r.stderr = w }

func defaultRunner() *jailbreakRunner {
	return &jailbreakRunner{
		execCommand: func(name string, args ...string) cmdRunner {
			cmd := execCommand(name, args...)
			return &realCmdRunner{
				cmd:    cmd,
				stdout: &cmd.Stdout,
				stderr: &cmd.Stderr,
			}
		},
		readFile:  os.ReadFile,
		writeFile: os.WriteFile,
		appendFile: func(path string, data []byte) error {
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = f.Write(data)
			return err
		},
		mkdirAll:     os.MkdirAll,
		stat:         os.Stat,
		removeFile:   os.Remove,
		removeAll:    os.RemoveAll,
		rename:       os.Rename,
		realpath:     filepath.EvalSymlinks,
		copyFile:     copyFile,
		getMountInfo: getMountInfoFromSystem,
		remountRW: func(mnt string) error {
			cmd := execCommand("mount", "-o", "remount,rw", mnt)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		},
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
	}
}

// getMountInfoFromSystem uses findmnt to get UUID and FSTYPE for a mount path.
func getMountInfoFromSystem(mnt string) (*mountInfo, error) {
	if mnt == "" {
		return nil, fmt.Errorf("missing mount path parameter")
	}

	uuid, err := fslib.MountpointToUUID(mnt)
	if err != nil {
		return nil, fmt.Errorf("cannot determine UUID for %s: %w", mnt, err)
	}

	fstype, err := fslib.MountpointToFSType(mnt)
	if err != nil {
		return nil, fmt.Errorf("cannot determine FSTYPE for %s: %w", mnt, err)
	}

	return &mountInfo{UUID: uuid, FSType: fstype}, nil
}

// JailbreakCommand converts a matrixOS OSTree deployment into a mutable Gentoo install.
type JailbreakCommand struct {
	BaseCommand
	UI
	fs  *flag.FlagSet
	run *jailbreakRunner
}

// NewJailbreakCommand creates a new JailbreakCommand.
func NewJailbreakCommand() ICommand {
	return &JailbreakCommand{}
}

// Name returns the name of the command.
func (c *JailbreakCommand) Name() string {
	return "jailbreak"
}

// Init initializes the command.
func (c *JailbreakCommand) Init(args []string) error {
	if err := c.initClientConfig(); err != nil {
		return err
	}
	if err := c.initOstree(); err != nil {
		return err
	}
	c.StartUI()
	c.run = defaultRunner()
	return c.parseArgs(args)
}

func (c *JailbreakCommand) parseArgs(args []string) error {
	c.fs = flag.NewFlagSet("jailbreak", flag.ContinueOnError)
	c.fs.Usage = func() {
		fmt.Printf("Usage: vector %s\n", c.Name())
		c.fs.PrintDefaults()
	}
	return c.fs.Parse(args)
}

// configStr fetches a mandatory string value from config, returning an error if missing.
func (c *JailbreakCommand) configStr(key string) (string, error) {
	val, err := c.cfg.GetItem(key)
	if err != nil {
		return "", fmt.Errorf("config key %s: %w", key, err)
	}
	if val == "" {
		return "", fmt.Errorf("config key %s is empty", key)
	}
	return val, nil
}

// Run executes the jailbreak operation.
func (c *JailbreakCommand) Run() error {
	if getEuid() != 0 {
		return fmt.Errorf("this command must be run as root")
	}

	sysroot, err := c.configStr("Ostree.Sysroot")
	if err != nil {
		return err
	}
	bootRoot, err := c.configStr("Imager.BootRoot")
	if err != nil {
		return err
	}
	efiRoot, err := c.configStr("Imager.EfiRoot")
	if err != nil {
		return err
	}
	fullSuffix, err := c.configStr("Ostree.FullBranchSuffix")
	if err != nil {
		return err
	}

	c.printTitle(fullSuffix)
	c.printGiantWarning()

	if err := c.sanityChecks(sysroot, bootRoot, efiRoot, fullSuffix); err != nil {
		return err
	}
	if err := c.remountSysroot(sysroot); err != nil {
		return err
	}
	if err := c.cloneToSysroot(sysroot); err != nil {
		return err
	}
	if err := c.generateFstab(sysroot, bootRoot, efiRoot); err != nil {
		return err
	}
	if err := c.bootloaderSetup(bootRoot); err != nil {
		return err
	}
	if err := c.cleanConfig(sysroot, bootRoot, efiRoot); err != nil {
		return err
	}
	if err := c.syncPortage(sysroot); err != nil {
		return err
	}
	if err := c.cleanPackages(); err != nil {
		return err
	}

	fmt.Fprintln(c.run.stdout, "All done. Try rebooting now. Good luck with emerge!")
	return nil
}

func (c *JailbreakCommand) printTitle(fullSuffix string) {
	w := c.run.stderr
	fmt.Fprintln(w, "===============================================================================")
	fmt.Fprintln(w, "    matrixOS JAILBREAKING: OSTREE -> MUTABLE GENTOO")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "    ...aka. turn your system into a regular Gentoo install")
	fmt.Fprintln(w, "    so that you can feel like a real hacker!")
	fmt.Fprintln(w)
	fmt.Fprintf(w, " PLEASE MAKE SURE to run (and then reboot!), see README.md for more details:\n")
	fmt.Fprintf(w, "   # ostree admin switch matrixos/<your branch>-%s\n", fullSuffix)
	fmt.Fprintln(w, "===============================================================================")
	fmt.Fprintln(w)
}

func (c *JailbreakCommand) printGiantWarning() {
	w := c.run.stderr
	fmt.Fprintln(w, "WARNING: This will clone the current OS to the physical disk")
	fmt.Fprintln(w, "and detach from the OSTree deployment. IT CANNOT BE UNDONE (easily).")
	fmt.Fprintln(w, "WARNING: If something goes wrong, you will end up with a DESTROYED")
	fmt.Fprintln(w, "system. So, BACK EVERYTHING UP and buckle up!")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "This operation clones the current system and should carry all your data over.")
	fmt.Fprintln(w, "However, it may contain bugs or wrong assumptions. The matrixOS team is not going")
	fmt.Fprintln(w, "to be responsible for your potentially incurred data loss and by running this tool")
	fmt.Fprintln(w, "you accept the risk.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "To perform the cloning, this tool will not use much additional space.")
	fmt.Fprintln(w, "This means that you won't need 2x the used disk space to perform this operation.")
	fmt.Fprintln(w)
}

func (c *JailbreakCommand) sanityChecks(sysroot, bootRoot, efiRoot, fullSuffix string) error {
	// Check sysroot exists.
	if _, err := c.run.stat(sysroot); err != nil {
		return fmt.Errorf("%s does not exist", sysroot)
	}

	// Check we're on a -full branch.
	deployments, err := c.ot.ListDeployments(false)
	if err != nil {
		return fmt.Errorf("failed to list deployments: %w", err)
	}
	onFull := false
	for _, dep := range deployments {
		if dep.Booted && strings.Contains(dep.Refspec, fullSuffix) {
			onFull = true
			break
		}
	}
	if !onFull {
		fmt.Fprintf(c.run.stderr,
			"You have not switched to a -%s ostree branch. Please read the instructions above.\n",
			fullSuffix)
		// Show available full branches.
		fmt.Fprintln(c.run.stderr, "Showing available full ostree branches:")
		refs, err := c.ot.RemoteRefs(false)
		if err == nil {
			for _, ref := range refs {
				if strings.HasSuffix(ref, "-"+fullSuffix) {
					fmt.Fprintln(c.run.stderr, ref)
				}
			}
		}
		return fmt.Errorf("not on a -%s ostree branch", fullSuffix)
	}

	// Check that ReadOnlyVdb or /var/db/pkg exists.
	roVdb, _ := c.cfg.GetItem("Releaser.ReadOnlyVdb")
	_, roVdbErr := c.run.stat(roVdb)
	_, varDbErr := c.run.stat("/var/db/pkg")
	if roVdbErr != nil && varDbErr != nil {
		return fmt.Errorf("you have not switched to a -%s ostree branch or must reboot first", fullSuffix)
	}

	// Check available disk space (at least 4 GiB).
	dfCmd := c.run.execCommand("df", sysroot, "--output=avail", "--block-size=1000")
	dfOut, err := dfCmd.Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(dfOut)), "\n")
		if len(lines) >= 2 {
			avail := strings.TrimSpace(lines[len(lines)-1])
			var space int64
			fmt.Sscanf(avail, "%d", &space)
			if space > 0 && space < 4000000 {
				return fmt.Errorf("less than 4GiB of space available, cannot continue")
			}
		}
	} else {
		fmt.Fprintln(c.run.stderr, "WARNING: Unable to determine the free space available. Use at your own risk")
	}

	// Verify mount info can be obtained for all critical paths.
	for _, dev := range []string{sysroot, bootRoot, efiRoot} {
		if _, err := c.run.getMountInfo(dev); err != nil {
			return fmt.Errorf("mount info check failed for %s: %w", dev, err)
		}
	}

	// Confirmation prompt.
	fmt.Fprint(c.run.stdout, "Type 'DESTROYALL' to continue: ")
	scanner := bufio.NewScanner(c.run.stdin)
	if scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "DESTROYALL" {
			return fmt.Errorf("aborted")
		}
	} else {
		return fmt.Errorf("aborted: no input")
	}

	return nil
}

func (c *JailbreakCommand) remountSysroot(sysroot string) error {
	fmt.Fprintln(c.run.stdout, "Remounting physical root filesystem read/write ...")
	return c.run.remountRW(sysroot)
}

func (c *JailbreakCommand) cloneToSysroot(sysroot string) error {
	// Find currently booted deployment.
	deployments, err := c.ot.ListDeployments(false)
	if err != nil {
		return fmt.Errorf("failed to list deployments: %w", err)
	}
	var booted *cds.Deployment
	for i := range deployments {
		if deployments[i].Booted {
			booted = &deployments[i]
			break
		}
	}
	if booted == nil {
		return fmt.Errorf("unable to find booted deployment")
	}

	osName, err := c.configStr("matrixOS.OsName")
	if err != nil {
		return err
	}

	deploymentDir := filepath.Join(
		"/ostree/deploy", osName, "deploy",
		booted.Checksum+"."+fmt.Sprint(booted.Serial),
	)
	if _, err := c.run.stat(deploymentDir); err != nil {
		return fmt.Errorf("unable to find deployment dir %s: %w", deploymentDir, err)
	}

	fmt.Fprintf(c.run.stdout, "Found currently deployed ostree commit: %s ...\n", booted.Checksum)
	fmt.Fprintf(c.run.stdout, "Cloning your current install of matrixOS to %s ...\n", sysroot)

	// Use cpio to clone, excluding certain paths.
	cpioCmd := c.run.execCommand("sh", "-c",
		fmt.Sprintf(
			`cd %q && find . -xdev -depth `+
				`-not -path "./sysroot*" `+
				`-not -path "./ostree*" `+
				`-not -path "./mnt*" `+
				`-not -path "./var/lib/nfs*" `+
				`-not -path "./tmp*" `+
				`-not -path "./run*" `+
				`-not -path "./sys*" `+
				`-not -path "./proc*" `+
				`-not -path "./dev*" `+
				`-printf '%%P\0' | cpio --null -pd0lu %q`,
			deploymentDir, sysroot,
		),
	)
	cpioCmd.SetStdout(c.run.stdout)
	cpioCmd.SetStderr(c.run.stderr)
	if err := cpioCmd.Run(); err != nil {
		return fmt.Errorf("failed to clone deployment to sysroot: %w", err)
	}

	// Restore /efi and /boot directories.
	for _, dir := range []string{"efi", "boot"} {
		p := filepath.Join(sysroot, dir)
		fmt.Fprintf(c.run.stdout, "Restoring /%s...\n", dir)
		c.run.mkdirAll(p, 0755)
	}

	// Remove immutable bits.
	fmt.Fprintln(c.run.stdout, "Unlocking files (removing immutable bit) ...")
	chattrCmd := c.run.execCommand("chattr", "-R", "-i", sysroot+"/")
	chattrCmd.Run() // best effort

	return nil
}

func (c *JailbreakCommand) generateFstab(sysroot, bootRoot, efiRoot string) error {
	fmt.Fprintln(c.run.stdout, "Generating /etc/fstab ...")

	rootInfo, err := c.run.getMountInfo(sysroot)
	if err != nil {
		return fmt.Errorf("fstab: %w", err)
	}
	bootInfo, err := c.run.getMountInfo(bootRoot)
	if err != nil {
		return fmt.Errorf("fstab: %w", err)
	}
	efiInfo, err := c.run.getMountInfo(efiRoot)
	if err != nil {
		return fmt.Errorf("fstab: %w", err)
	}

	fstabPath := filepath.Join(sysroot, "etc", "fstab")
	var fstab strings.Builder
	fmt.Fprintf(&fstab, "UUID=%s / %s defaults 0 1\n", rootInfo.UUID, rootInfo.FSType)
	fmt.Fprintf(&fstab, "UUID=%s %s %s defaults 0 1\n", bootInfo.UUID, bootRoot, bootInfo.FSType)
	fmt.Fprintf(&fstab, "UUID=%s %s %s defaults 0 1\n", efiInfo.UUID, efiRoot, efiInfo.FSType)

	return c.run.appendFile(fstabPath, []byte(fstab.String()))
}

func (c *JailbreakCommand) bootloaderSetup(bootRoot string) error {
	// Parse boot args from /proc/cmdline.
	cmdlineData, err := c.run.readFile("/proc/cmdline")
	if err != nil {
		return fmt.Errorf("failed to read /proc/cmdline: %w", err)
	}

	args := strings.Fields(strings.TrimSpace(string(cmdlineData)))
	var bootedKernel string
	var kernelBootArgs []string
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "BOOT_IMAGE="):
			bootedKernel = strings.TrimPrefix(arg, "BOOT_IMAGE=")
		case arg == "rw":
			continue
		case strings.HasPrefix(arg, "ostree="):
			continue
		case strings.HasPrefix(arg, "systemd.mount-extra="):
			continue
		default:
			kernelBootArgs = append(kernelBootArgs, arg)
		}
	}

	// Resolve kernel path.
	if _, err := c.run.stat(bootedKernel); err != nil {
		bootedKernel = "/boot" + bootedKernel
	}
	if _, err := c.run.stat(bootedKernel); err != nil {
		return fmt.Errorf("unable to find booted kernel at %s (from BOOT_IMAGE in /proc/cmdline)", bootedKernel)
	}
	bootedKernel, err = c.run.realpath(bootedKernel)
	if err != nil {
		return fmt.Errorf("failed to resolve kernel path: %w", err)
	}

	fmt.Fprintln(c.run.stdout, "Copying booted kernel ...")
	kernelName := filepath.Base(bootedKernel)
	newKernelName := strings.Replace(kernelName, "vmlinuz-", "kernel-", 1)
	bootKernelPath := filepath.Join("/boot", newKernelName)
	if err := c.run.copyFile(bootedKernel, bootKernelPath); err != nil {
		return fmt.Errorf("failed to copy kernel: %w", err)
	}

	// Handle initramfs.
	initramfsName := strings.Replace(kernelName, "vmlinuz-", "initramfs-", 1)
	kernelDir := filepath.Dir(bootedKernel)
	initramfsPath := filepath.Join(kernelDir, initramfsName)
	if _, err := c.run.stat(initramfsPath); err != nil {
		initramfsPath = initramfsPath + ".img"
	}

	var initramfsBootPath string
	if _, err := c.run.stat(initramfsPath); err == nil {
		initramfsPath, _ = c.run.realpath(initramfsPath)
		initramfsBootPath = filepath.Join("/boot", filepath.Base(initramfsPath))
		if err := c.run.copyFile(initramfsPath, initramfsBootPath); err != nil {
			return fmt.Errorf("failed to copy initramfs: %w", err)
		}
	} else {
		fmt.Fprintln(c.run.stderr, "Initramfs not found, ignoring ...")
	}

	// Write BLS entry.
	blsEntry, err := c.configStr("Jailbreak.BootLoaderEntry")
	if err != nil {
		return err
	}

	entriesDir := filepath.Join(bootRoot, "loader", "entries")
	c.run.mkdirAll(entriesDir, 0755)
	blsCfgPath := filepath.Join(entriesDir, blsEntry)

	fmt.Fprintf(c.run.stdout,
		"Setting up %s/loader/entries with following kernel boot params: %s ...\n",
		bootRoot, strings.Join(kernelBootArgs, " "))

	var bls strings.Builder
	fmt.Fprintln(&bls, "title matrixOS (Gentoo-based, jailbroken)")
	fmt.Fprintln(&bls, "version 1")
	fmt.Fprintf(&bls, "options %s\n", strings.Join(kernelBootArgs, " "))
	fmt.Fprintf(&bls, "linux %s\n", strings.TrimPrefix(bootKernelPath, bootRoot))
	if initramfsBootPath != "" {
		fmt.Fprintf(&bls, "initrd %s\n", strings.TrimPrefix(initramfsBootPath, bootRoot))
	}

	if err := c.run.writeFile(blsCfgPath, []byte(bls.String()), 0644); err != nil {
		return fmt.Errorf("failed to write BLS config: %w", err)
	}

	fmt.Fprintln(c.run.stdout, "Final bls bootloader config:")
	fmt.Fprint(c.run.stdout, bls.String())
	fmt.Fprintln(c.run.stdout, "--")

	return nil
}

func (c *JailbreakCommand) cleanConfig(sysroot, bootRoot, efiRoot string) error {
	if err := c.cleanConfigSetupBLS(bootRoot); err != nil {
		return err
	}
	if err := c.cleanConfigSetupSystemdRepart(sysroot); err != nil {
		return err
	}
	if err := c.cleanConfigSetupVarDbPkg(sysroot); err != nil {
		return err
	}
	if err := c.cleanConfigFixSrv(sysroot); err != nil {
		return err
	}
	return c.cleanConfigSetupSecurebootKeys(efiRoot)
}

func (c *JailbreakCommand) cleanConfigSetupBLS(bootRoot string) error {
	fmt.Fprintf(c.run.stdout, "Removing old %s/loader/entries/ configs ...\n", bootRoot)
	for _, old := range []string{"ostree-1.conf", "ostree-2.conf"} {
		c.run.removeFile(filepath.Join(bootRoot, "loader", "entries", old))
	}
	return nil
}

func (c *JailbreakCommand) cleanConfigSetupSystemdRepart(sysroot string) error {
	fmt.Fprintln(c.run.stdout, "Disabling systemd-repart config ...")
	c.run.removeFile(filepath.Join(sysroot, "etc", "repart.d", "50-matrixos-rootfs.conf"))
	return nil
}

func (c *JailbreakCommand) cleanConfigSetupVarDbPkg(sysroot string) error {
	fmt.Fprintln(c.run.stdout, "Setting up /var/db/pkg ...")
	roVdb, err := c.configStr("Releaser.ReadOnlyVdb")
	if err != nil {
		return err
	}

	// Remove any symlink at /var/db/pkg, then move the read-only VDB into place.
	varDbPkg := filepath.Join(sysroot, "var", "db", "pkg")
	c.run.removeAll(varDbPkg)

	src := filepath.Join(strings.TrimRight(sysroot, "/"), roVdb)
	return c.run.rename(src, varDbPkg)
}

func (c *JailbreakCommand) cleanConfigFixSrv(sysroot string) error {
	fmt.Fprintln(c.run.stdout, "Fixing /srv ...")
	srv := filepath.Join(sysroot, "srv")

	info, err := c.run.stat(srv)
	if err != nil {
		// Does not exist at all — create it.
		c.run.mkdirAll(srv, 0755)
		c.run.writeFile(filepath.Join(srv, ".keep"), []byte{}, 0644)
		return nil
	}

	// If it's a dangling symlink, replace it with a directory.
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		c.run.removeFile(srv)
		c.run.mkdirAll(srv, 0755)
		c.run.writeFile(filepath.Join(srv, ".keep"), []byte{}, 0644)
	}
	return nil
}

func (c *JailbreakCommand) cleanConfigSetupSecurebootKeys(efiRoot string) error {
	certPath, err := c.cfg.GetItem("Seeder.DefaultSecureBootPublicKey")
	if err != nil {
		fmt.Fprintf(c.run.stderr, "WARNING: SecureBoot cert config not found: %v\n", err)
		return nil
	}

	fmt.Fprintln(c.run.stdout, "matrixOS uses its own secureboot key pair.")
	fmt.Fprintln(c.run.stdout, "For jailbroken systems, we need to create a separate set of keys.")

	if _, err := c.run.stat(efiRoot); err != nil {
		fmt.Fprintf(c.run.stderr, "WARNING: %s not found, skipping SecureBoot automatic MOK generation\n", efiRoot)
		return nil
	}

	if _, err := c.run.stat(certPath); err != nil {
		fmt.Fprintf(c.run.stderr, "WARNING: Unable to find SecureBoot certificate at: %s\n", certPath)
		return nil
	}

	fmt.Fprintln(c.run.stdout, "Creating a new MOK file to ease shim MOK keys loading ...")
	mokPath := filepath.Join(efiRoot, "matrixos-jailbroken-secureboot-cert.mok")
	opensslCmd := c.run.execCommand("openssl", "x509", "-in", certPath, "-outform", "DER", "-out", mokPath)
	opensslCmd.SetStdout(c.run.stdout)
	opensslCmd.SetStderr(c.run.stderr)
	if err := opensslCmd.Run(); err != nil {
		fmt.Fprintf(c.run.stderr, "WARNING: Failed to generate MOK file: %v\n", err)
	}

	return nil
}

func (c *JailbreakCommand) syncPortage(sysroot string) error {
	fmt.Fprintln(c.run.stdout, "Let me prep the Portage tree for ya... Downloading Portage ...")

	// emerge-webrsync (best effort).
	webrsyncCmd := c.run.execCommand("emerge-webrsync")
	webrsyncCmd.SetStdout(c.run.stdout)
	webrsyncCmd.SetStderr(c.run.stderr)
	webrsyncCmd.Run() // best effort

	// Clone overlay repositories that use git.
	reposConfPath := filepath.Join(sysroot, "etc", "portage", "repos.conf", "eselect-repo.conf")
	reposData, err := c.run.readFile(reposConfPath)
	if err != nil {
		fmt.Fprintf(c.run.stderr, "WARNING: cannot read repos config: %v\n", err)
		return nil
	}

	ini, err := config.ParseIni(strings.NewReader(string(reposData)))
	if err != nil {
		fmt.Fprintf(c.run.stderr, "WARNING: cannot parse repos config: %v\n", err)
		return nil
	}

	for section, items := range ini {
		if section == "" {
			continue
		}
		if items["sync-type"] != "git" {
			fmt.Fprintf(c.run.stderr, "Repository %s does not use git. Not supported...\n", section)
			continue
		}
		repoDir := items["location"]
		gitURL := items["sync-uri"]
		if repoDir == "" || gitURL == "" {
			continue
		}

		fmt.Fprintf(c.run.stdout, "Cloning %s into %s for %s ...\n", gitURL, repoDir, section)
		gitCmd := c.run.execCommand("git", "clone", "--depth", "1", gitURL,
			filepath.Join(sysroot, repoDir))
		gitCmd.SetStdout(c.run.stdout)
		gitCmd.SetStderr(c.run.stderr)
		gitCmd.Run() // best effort
	}

	return nil
}

func (c *JailbreakCommand) cleanPackages() error {
	fmt.Fprintln(c.run.stdout, "Cleaning live/ostree packages ...")

	defaultUsername, _ := c.cfg.GetItem("matrixOS.DefaultUsername")

	// Check if user exists.
	idCmd := c.run.execCommand("id", "-u", defaultUsername)
	uidOut, err := idCmd.Output()
	uid := strings.TrimSpace(string(uidOut))

	// If user does not exist, unmerge the packages.
	if err != nil || uid == "" {
		pkgsToClean := []string{
			"acct-user/matrixos-live-home",
			"acct-user/matrixos-live",
			"virtual/matrixos-setup",
		}
		emergeCmd := c.run.execCommand("emerge",
			append([]string{"--depclean", "-v", "--with-bdeps=n"}, pkgsToClean...)...)
		emergeCmd.SetStdout(c.run.stdout)
		emergeCmd.SetStderr(c.run.stderr)
		emergeCmd.Run() // best effort
	}

	return nil
}
