package validation

import (
	"bytes"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"matrixos/vector/lib/config"
	"matrixos/vector/lib/filesystems"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	execCommand = exec.Command
	lookPath    = exec.LookPath
)

type QA struct {
	cfg config.IConfig
}

// New creates a new QA instance.
func New(cfg config.IConfig) (*QA, error) {
	if cfg == nil {
		return nil, errors.New("missing config parameter")
	}
	return &QA{
		cfg: cfg,
	}, nil
}

// RootPrivs checks the current process is root. Returns error if not.
func (q *QA) RootPrivs() error {
	if os.Geteuid() != 0 {
		return errors.New("Run as root")
	}
	return nil
}

// CheckMatrixOSPrivate verifies PrivateGitRepoPath exists and is a directory.
func (q *QA) CheckMatrixOSPrivate() error {
	path, err := q.cfg.GetItem("matrixOS.PrivateGitRepoPath")
	if err != nil {
		return err
	}
	if path == "" {
		return errors.New("matrixOS.PrivateGitRepoPath not set")
	}
	st, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s does not exist: %w", path, err)
	}
	if !st.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	return nil
}

// CheckSecureBoot validates SecureBoot signing key of usb-storage kernel modules
// matches the serial in the provided certificate file. It requires `openssl` or
// can parse certificates directly.
func (q *QA) CheckSecureBoot(imageDir, sbcertPath string) error {
	modulesdir := filepath.Join(imageDir, "lib/modules")
	var usbMods []string
	// Walk the modulesdir to find usb-storage.ko*
	_ = filepath.WalkDir(modulesdir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.HasPrefix(filepath.Base(p), "usb-storage.ko") {
			usbMods = append(usbMods, p)
		}
		return nil
	})
	if len(usbMods) == 0 {
		return fmt.Errorf("No usb-storage.ko found in %s", modulesdir)
	}

	// Parse cert serial from PEM
	serial, err := certSerialColon(sbcertPath)
	if err != nil {
		return fmt.Errorf("Cannot extract SecureBoot serial from %s: %w", sbcertPath, err)
	}

	for _, mod := range usbMods {
		rel := strings.TrimPrefix(mod, strings.TrimRight(imageDir, "/"))

		cmd, err := filesystems.ChrootCmd(imageDir, "modinfo", "-F", "sig_key", rel)
		if err != nil {
			return fmt.Errorf("chroot %s failed: %v", imageDir, err)
		}
		out, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("chroot modinfo failed for %s: %w", rel, err)
		}

		sig := strings.TrimSpace(string(out))
		if sig == "" {
			return fmt.Errorf("No sig_key found for %s", rel)
		}

		if serial != sig {
			return fmt.Errorf(
				"%s SecureBoot serial and module signature key mismatch: cert='%s' module='%s'",
				rel, serial, sig,
			)
		}
	}
	return nil
}

// certSerialColon reads a certificate file (PEM or DER) and returns the serial
// number formatted as hex bytes separated by colons (aa:bb:cc...)
func certSerialColon(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	// try PEM decode
	if bytes.HasPrefix(b, []byte("-----BEGIN")) {
		blk, _ := pem.Decode(b)
		if blk == nil {
			return "", errors.New("failed to decode PEM")
		}
		b = blk.Bytes
	}
	cert, err := x509.ParseCertificate(b)
	if err != nil {
		return "", err
	}
	// serial is big.Int; get bytes
	s := cert.SerialNumber.Bytes()
	if len(s) == 0 {
		return "", errors.New("empty serial")
	}
	hexs := hex.EncodeToString(s)
	// ensure even length
	if len(hexs)%2 != 0 {
		hexs = "0" + hexs
	}
	parts := make([]string, 0, len(hexs)/2)
	for i := 0; i < len(hexs); i += 2 {
		parts = append(parts, hexs[i:i+2])
	}
	return strings.ToLower(strings.Join(parts, ":")), nil
}

// verifyEnvironmentSetup checks for the presence of executables and directories
// inside an image directory. executables should be the list as used in the
// shell script and dirs is the list of directories to verify (absolute paths).
func verifyEnvironmentSetup(imageDir string, executables, dirs []string) error {
	if imageDir == "" {
		return errors.New("missing parameter imageDir")
	}

	var retErrs []string
	for _, exe := range executables {
		// path-like executable
		if strings.Contains(exe, "/") {
			// check file inside root
			target := exe
			if imageDir != "/" {
				target = imageDir + exe
			}

			if st, err := os.Stat(target); err != nil || st.Mode()&0111 == 0 {
				retErrs = append(retErrs, fmt.Sprintf("%s not found", target))
			}
			continue
		}

		// Simple command name (e.g. "sh")
		if imageDir == "/" {
			if _, err := lookPath(exe); err != nil {
				retErrs = append(retErrs, fmt.Sprintf("%s not found", exe))
			}
			continue
		}

		// First, check common bin locations inside the image
		tryPaths := []string{
			imageDir + "/bin/" + exe,
			imageDir + "/usr/bin/" + exe,
		}

		found := false
		for _, p := range tryPaths {
			if st, err := os.Stat(p); err == nil && st.Mode()&0111 != 0 {
				found = true
				break
			}
		}

		if !found {
			// fallback to chroot when available
			cmd, err := filesystems.ChrootCmd(imageDir, "which", exe)
			if err != nil {
				retErrs = append(
					retErrs,
					fmt.Sprintf("chroot %s failed: %v", imageDir, err),
				)
				continue
			}
			if out, err := cmd.Output(); err != nil || len(bytes.TrimSpace(out)) == 0 {
				retErrs = append(
					retErrs,
					fmt.Sprintf("%s not found in chroot %s", exe, imageDir),
				)
			}
		}
	}

	for _, d := range dirs {
		p := filepath.Join(imageDir, d)
		if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
			retErrs = append(retErrs, fmt.Sprintf("%s not found", p))
		}
	}

	if len(retErrs) > 0 {
		return errors.New(strings.Join(retErrs, "; "))
	}
	return nil
}

// VerifyDistroRootfsEnvironmentSetup checks the typical distro binaries and dirs
func (q *QA) VerifyDistroRootfsEnvironmentSetup(imageDir string) error {
	if imageDir == "" {
		return errors.New("missing parameter imageDir")
	}
	executables := []string{
		"blockdev",
		"btrfs",
		"chroot",
		"cryptsetup",
		"efibootmgr",
		"find",
		"findmnt",
		"fstrim",
		"gpg",
		"losetup",
		"mkfs.btrfs",
		"mkfs.vfat",
		"openssl",
		"ostree",
		"partprobe",
		"qemu-img",
		"sha256sum",
		"sgdisk",
		"udevadm",
		"unshare",
		"wget",
		"xz",
	}
	if imageDir != "/" {
		executables = append(executables, "/usr/bin/grub-install")
	}
	dirs := []string{"/usr/share/shim"}
	return verifyEnvironmentSetup(imageDir, executables, dirs)
}

// VerifyReleaserEnvironmentSetup checks for tools required by releaser
func (q *QA) VerifyReleaserEnvironmentSetup(imageDir string) error {
	if imageDir == "" {
		return errors.New("missing parameter imageDir")
	}
	executables := []string{
		"chroot",
		"find",
		"findmnt",
		"gpg",
		"openssl",
		"ostree",
		"unshare",
	}

	path, err := q.cfg.GetItem("matrixOS.PrivateGitRepoPath")
	if err != nil {
		return err
	}
	dirs := []string{path}
	return verifyEnvironmentSetup(imageDir, executables, dirs)
}

// VerifySeederEnvironmentSetup checks tools for seeder
func (q *QA) VerifySeederEnvironmentSetup(imageDir string) error {
	if imageDir == "" {
		return errors.New("missing parameter imageDir")
	}
	executables := []string{
		"chroot",
		"gpg",
		"openssl",
		"ostree",
		"unshare",
		"wget",
	}

	path, err := q.cfg.GetItem("matrixOS.PrivateGitRepoPath")
	if err != nil {
		return err
	}
	dirs := []string{path}
	return verifyEnvironmentSetup(imageDir, executables, dirs)
}

// VerifyImagerEnvironmentSetup checks tools for imager
func (q *QA) VerifyImagerEnvironmentSetup(imageDir string, _gpgEnabled string) error {
	if imageDir == "" {
		return errors.New("missing parameter imageDir")
	}
	executables := []string{
		"blockdev",
		"btrfs",
		"chroot",
		"cryptsetup",
		"efibootmgr",
		"findmnt",
		"fstrim",
		"gpg",
		"grub-install",
		"losetup",
		"mkfs.vfat",
		"mkfs.btrfs",
		"openssl",
		"ostree",
		"partprobe",
		"qemu-img",
		"sha256sum",
		"sgdisk",
		"unshare",
		"udevadm",
		"xz",
	}
	dirs := []string{"/usr/share/shim"}
	return verifyEnvironmentSetup(imageDir, executables, dirs)
}

// CheckKernelAndExternalModule validates presence of kernels, initramfs and kernel
// modules matching moduleName (a glob pattern like "nvidia.ko*").
func (q *QA) CheckKernelAndExternalModule(imageDir, moduleName string) error {
	if moduleName == "" || imageDir == "" {
		return errors.New("missing parameters imageDir and module name")
	}
	modulesdir := filepath.Join(imageDir, "lib/modules")
	vmlinuzes, _ := filepath.Glob(filepath.Join(modulesdir, "*", "vmlinuz"))
	vmlinuzCount := len(vmlinuzes)
	for _, v := range vmlinuzes {
		fmt.Printf("Found kernel: %s\n", v)
	}
	initramfses, _ := filepath.Glob(filepath.Join(modulesdir, "*", "initramfs"))
	initramfsCount := len(initramfses)
	for _, i := range initramfses {
		fmt.Printf("Found initramfs: %s\n", i)
	}
	if vmlinuzCount == 0 {
		return errors.New("No kernel found. Refusing to release.")
	}
	if initramfsCount == 0 {
		return errors.New("No initramfs found. Refusing to release.")
	}
	if vmlinuzCount != initramfsCount {
		return fmt.Errorf(
			"vmlinuz found: %d -- initramfs found: %d. Refusing to release.",
			vmlinuzCount, initramfsCount,
		)
	}

	// Find kernel modules matching moduleName
	var kernelMods []string
	_ = filepath.WalkDir(modulesdir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			if ok, _ := filepath.Match(moduleName, filepath.Base(p)); ok {
				kernelMods = append(kernelMods, p)
			}
		}
		return nil
	})
	if len(kernelMods) == 0 {
		return fmt.Errorf("No %s found in %s", moduleName, modulesdir)
	}

	modCount := 0
	var failure bool

	for _, kernelMod := range kernelMods {
		rel := strings.TrimPrefix(kernelMod, strings.TrimRight(imageDir, "/"))
		modCount++
		fmt.Printf("Testing module: %s\n", rel)
		cmd, err := filesystems.ChrootCmd(imageDir, "modinfo", "-F", "vermagic", rel)
		if err != nil {
			failure = true
			continue
		}
		out, err := cmd.Output()
		if err != nil {
			failure = true
			continue
		}

		kernelModVermagic := strings.TrimSpace(string(out))
		moduleKernelVer := strings.Fields(kernelModVermagic)[0]
		fmt.Printf(
			"%s: vermagic is: %s, kernel ver is: %s\n",
			rel,
			kernelModVermagic,
			moduleKernelVer,
		)

		correspondingVmlinuz := filepath.Join(modulesdir, moduleKernelVer, "vmlinuz")
		if _, err := os.Stat(correspondingVmlinuz); err != nil {
			fmt.Printf(
				"%s not found for related %s. Refusing to release.\n",
				correspondingVmlinuz,
				rel,
			)
			failure = true
			continue
		}

		// run file -b to extract version
		fout, err := execCommand("file", "-b", correspondingVmlinuz).Output()
		if err != nil {
			failure = true
			continue
		}

		re := regexp.MustCompile(`version (\S+)`)
		m := re.FindStringSubmatch(string(fout))
		if len(m) < 2 {
			failure = true
			continue
		}

		vmlinuzKernelVer := m[1]
		if vmlinuzKernelVer != moduleKernelVer {
			fmt.Printf(
				"%s: mismatch in kernel ver: (M) %s vs (K) %s\n",
				rel,
				moduleKernelVer,
				vmlinuzKernelVer,
			)
			failure = true
			continue
		}
	}
	if failure {
		return errors.New("kernel module checks failed")
	}
	if modCount != vmlinuzCount {
		return fmt.Errorf(
			"Unexpected number of %s files found! Refusing to release. "+
				"Number of %s modules: %d -- vmlinuz found: %d",
			moduleName, moduleName, modCount, vmlinuzCount,
		)
	}
	return nil
}

// CheckNvidiaModule checks whether nvidia drivers are installed and delegates to the
// kernel module check.
func (q *QA) CheckNvidiaModule(imageDir string) error {
	if imageDir == "" {
		return errors.New("missing parameter imageDir")
	}
	if fi, err := os.Stat(imageDir); err != nil || !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", imageDir)
	}
	// match glob var/db/pkg/x11-drivers/nvidia-drivers*
	matches, _ := filepath.Glob(
		filepath.Join(imageDir, "var/db/pkg/x11-drivers/nvidia-drivers*"),
	)
	if len(matches) == 0 {
		fmt.Println("x11-drivers/nvidia-drivers* not installed, skipping QA check")
		return nil
	}
	return q.CheckKernelAndExternalModule(imageDir, "nvidia.ko*")
}

// CheckRyzenSMUModule checks for ryzen_smu and delegates to module check
func (q *QA) CheckRyzenSMUModule(imageDir string) error {
	if imageDir == "" {
		return errors.New("missing parameter imageDir")
	}
	if fi, err := os.Stat(imageDir); err != nil || !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", imageDir)
	}
	matches, _ := filepath.Glob(
		filepath.Join(imageDir, "var/db/pkg/app-admin/ryzen_smu*"),
	)
	if len(matches) == 0 {
		fmt.Println("app-admin/ryzen_smu* not installed, skipping QA check")
		return nil
	}
	return q.CheckKernelAndExternalModule(imageDir, "ryzen_smu.ko*")
}

// CheckNumberOfKernels verifies the number of vmlinuz files under /usr/lib/modules
func (q *QA) CheckNumberOfKernels(imageDir string, expectedAmount int) error {
	if imageDir == "" {
		return errors.New("missing imageDir parameter")
	}
	if fi, err := os.Stat(imageDir); err != nil || !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", imageDir)
	}
	found := 0
	matches, _ := filepath.Glob(filepath.Join(imageDir, "usr/lib/modules/*/vmlinuz"))
	found = len(matches)
	if found != expectedAmount {
		fmt.Fprintf(os.Stderr, "Found %d kernels in /usr/lib/modules, expected %d\n", found, expectedAmount)
		// list directory
		entries, _ := os.ReadDir(filepath.Join(imageDir, "usr/lib/modules/"))
		for _, e := range entries {
			fmt.Fprintf(os.Stderr, "%s\n", e.Name())
		}
		return fmt.Errorf("found %d kernels, expected %d", found, expectedAmount)
	}
	return nil
}
