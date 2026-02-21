package filesystems

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

var (
	ExecCommand     = exec.Command
	devMapperPrefix = "/dev/mapper"
	sysMount        = unix.Mount
	sysUnmount      = unix.Unmount
	sysIoctl        = unix.Syscall
)

// BLKFLSBUF is the ioctl command to flush block device buffers.
// It is commonly 0x1261 on Linux.
const BLKFLSBUF = 0x1261

// PathMode represents the mode of a path.
type PathMode struct {
	Type   string      // E.g., "-", "d", "l"
	SetUID bool        // Set-user-ID bit
	SetGID bool        // Set-group-ID bit
	Sticky bool        // Sticky bit
	Perms  fs.FileMode // Stored as uint32, printed as octal
}

// PathInfo represents the information of a path in an ostree commit.
type PathInfo struct {
	Mode           *PathMode // Mode information of the path
	Uid            uint64    // User ID of the owner
	Gid            uint64    // Group ID of the owner
	Size           uint64    // Size of the file in bytes
	OSTreeChecksum string    // Checksum of the path if regular file
	Path           string    // Full path of the file
	Link           string    // Target of the symlink if Type is "l"
}

// Equals compares two PathInfo entries for metadata equality:
// type, permission bits, uid, gid, size, symlink target and checksums.
func (a *PathInfo) Equals(b *PathInfo) bool {
	if a.Mode.Type != b.Mode.Type {
		return false
	}
	if a.Mode.Perms != b.Mode.Perms {
		return false
	}
	if a.Mode.SetUID != b.Mode.SetUID || a.Mode.SetGID != b.Mode.SetGID || a.Mode.Sticky != b.Mode.Sticky {
		return false
	}
	if a.Uid != b.Uid || a.Gid != b.Gid {
		return false
	}
	if a.Size != b.Size {
		return false
	}
	if a.Link != b.Link {
		return false
	}
	aCksum := "0"
	bCksum := "0"
	if a.Mode.Type == "-" {
		aCksum = a.OSTreeChecksum
	}
	if b.Mode.Type == "-" {
		bCksum = b.OSTreeChecksum
	}
	if aCksum != bCksum {
		return false
	}
	return true
}

// String returns a short human-readable description of a PathInfo.
func (pi *PathInfo) String() string {
	if pi == nil {
		return "(absent)"
	}
	typ := "file"
	switch pi.Mode.Type {
	case "d":
		typ = "dir"
	case "l":
		typ = fmt.Sprintf("link -> %s", pi.Link)
	}
	return fmt.Sprintf("%s %04o uid=%d gid=%d size=%d, csum=%s",
		typ, pi.Mode.Perms, pi.Uid, pi.Gid, pi.Size, pi.OSTreeChecksum)
}

// ListContents lists the contents of a path on the filesystem.
// It walks the directory tree recursively and returns information
// about regular files, directories, and symlinks, ignoring everything else.
func ListContents(path string) ([]*PathInfo, error) {
	if path == "" {
		return nil, fmt.Errorf("missing path parameter")
	}

	otRegFileChecksum := func(p string) string {
		ck, err := OstreeChecksumFileAt(p, OstreeObjectTypeFile, OstreeChecksumFlagsNone)
		if err != nil {
			log.Printf("WARNING: failed to compute OSTree checksum for %s: %v. Using dummy checksum.\n", p, err)
			return "0"
		}
		return ck
	}

	var pis []*PathInfo

	err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		mode := info.Mode()
		ft := mode.Type()

		var typeStr string
		var otChksum string
		switch {
		case ft.IsRegular():
			typeStr = "-"
			otChksum = otRegFileChecksum(p)
		case ft.IsDir():
			typeStr = "d"
		case ft&fs.ModeSymlink != 0:
			typeStr = "l"
		default:
			// Ignore anything that is not a regular file, directory, or symlink
			return nil
		}

		pm := &PathMode{
			Type:   typeStr,
			SetUID: mode&fs.ModeSetuid != 0,
			SetGID: mode&fs.ModeSetgid != 0,
			Sticky: mode&fs.ModeSticky != 0,
			Perms:  mode.Perm(),
		}

		pi := &PathInfo{
			Mode:           pm,
			Size:           uint64(info.Size()),
			Path:           p,
			OSTreeChecksum: otChksum,
		}

		// Get UID/GID from the underlying syscall stat
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			pi.Uid = uint64(stat.Uid)
			pi.Gid = uint64(stat.Gid)
		}

		// Resolve symlink target
		if typeStr == "l" {
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			pi.Link = target
		}

		pis = append(pis, pi)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return pis, nil
}

// DevicesSettle waits for udev events to settle.
func DevicesSettle() {
	ExecCommand("udevadm", "settle").Run()
}

// FlushBlockDeviceBuffers flushes the buffers of a block device.
func FlushBlockDeviceBuffers(devPath string) error {
	if devPath == "" {
		return fmt.Errorf("missing devPath parameter")
	}

	f, err := os.Open(devPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, _, errno := sysIoctl(unix.SYS_IOCTL, f.Fd(), uintptr(BLKFLSBUF), 0); errno != 0 {
		return fmt.Errorf("ioctl BLKFLSBUF failed: %w", errno)
	}
	return nil
}

// GetLuksRootfsDevicePath returns the device path for a given LUKS name.
func GetLuksRootfsDevicePath(luksName string) (string, error) {
	if luksName == "" {
		return "", fmt.Errorf("missing luksName parameter")
	}
	return filepath.Join(devMapperPrefix, luksName), nil
}

// DeviceUUID returns the UUID of a given device path.
func DeviceUUID(devPath string) (string, error) {
	if devPath == "" {
		return "", fmt.Errorf("missing argument devpath")
	}
	cmd := ExecCommand("blkid", "-s", "UUID", "-o", "value", devPath)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	trimmedOut := strings.TrimSpace(string(out))
	if trimmedOut == "" {
		return "", fmt.Errorf("could not find UUID for %s", devPath)
	}
	return trimmedOut, nil
}

// DevicePartUUID returns the PARTUUID of a given device path.
func DevicePartUUID(devPath string) (string, error) {
	if devPath == "" {
		return "", fmt.Errorf("missing argument devpath")
	}
	cmd := ExecCommand("blkid", "-s", "PARTUUID", "-o", "value", devPath)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	trimmedOut := strings.TrimSpace(string(out))
	if trimmedOut == "" {
		return "", fmt.Errorf("could not find PARTUUID for %s", devPath)
	}
	return trimmedOut, nil
}

// MountpointToDevice returns the device path for a given mountpoint.
func MountpointToDevice(mnt string) (string, error) {
	if mnt == "" {
		return "", fmt.Errorf("missing mnt parameter")
	}

	cmd := ExecCommand("findmnt", "-no", "SOURCES", mnt)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	trimmedOut := strings.TrimSpace(string(out))
	if trimmedOut == "" {
		return "", fmt.Errorf("no device found for mountpoint %s", mnt)
	}
	return strings.Split(trimmedOut, "\n")[0], nil
}

// MountpointToUUID returns the UUID for a given mountpoint.
func MountpointToUUID(mnt string) (string, error) {
	if mnt == "" {
		return "", fmt.Errorf("missing mnt parameter")
	}

	cmd := ExecCommand("findmnt", "-n", "-o", "UUID", "-T", mnt)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	trimmedOut := strings.TrimSpace(string(out))
	if trimmedOut == "" {
		return "", fmt.Errorf("no UUID found for mountpoint %s", mnt)
	}
	return trimmedOut, nil
}

// MountpointToFSType returns the filesystem type for a given mountpoint.
func MountpointToFSType(mnt string) (string, error) {
	if mnt == "" {
		return "", fmt.Errorf("missing mnt parameter")
	}

	cmd := ExecCommand("findmnt", "-n", "-o", "FSTYPE", "-T", mnt)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	trimmedOut := strings.TrimSpace(string(out))
	if trimmedOut == "" {
		return "", fmt.Errorf("no FSTYPE found for mountpoint %s", mnt)
	}
	return trimmedOut, nil
}

// CleanupMounts unmounts a list of mounts in reverse order.
func CleanupMounts(mounts []string) {
	DevicesSettle()
	for i := len(mounts) - 1; i >= 0; i-- {
		mnt := mounts[i]
		out, _ := ExecCommand("findmnt", "-n", mnt).Output()
		if len(out) == 0 {
			continue
		}
		log.Printf("Unmounting %s ...", mnt)
		if err := sysUnmount(mnt, 0); err != nil {
			FlushBlockDeviceBuffers(mnt)
			log.Printf("Unable to umount %s: %v", mnt, err)
			out, _ := ExecCommand("findmnt", mnt).CombinedOutput()
			log.Println(string(out))
			log.Printf("For safety, calling umount -l %s", mnt)
			sysUnmount(mnt, unix.MNT_DETACH)
			continue
		}
	}
	DevicesSettle()
}

// CleanupCryptsetupDevices closes a list of cryptsetup devices.
func CleanupCryptsetupDevices(devices []string) {
	DevicesSettle()
	for _, cd := range devices {
		cdpath, err := GetLuksRootfsDevicePath(cd)
		if err != nil {
			log.Println(err)
			continue
		}
		if _, err := os.Stat(cdpath); os.IsNotExist(err) {
			continue
		}

		log.Printf("Closing LUKS device: %s ...", cd)
		FlushBlockDeviceBuffers(cdpath)
		if err := ExecCommand("cryptsetup", "close", cd).Run(); err != nil {
			log.Printf("Unable to cryptsetup close %s", cdpath)
			out, _ := ExecCommand("findmnt", cdpath).CombinedOutput()
			log.Println(string(out))
			continue
		}
	}
	DevicesSettle()
}

// CleanupLoopDevices detaches a list of loop devices.
func CleanupLoopDevices(devices []string) {
	DevicesSettle()
	for _, ld := range devices {
		if _, err := os.Stat(ld); os.IsNotExist(err) {
			continue
		}
		out, _ := ExecCommand("losetup", "--raw", "-l", "-O", "BACK-FILE", ld).Output()
		if len(strings.TrimSpace(string(out))) == 0 {
			continue
		}

		log.Printf("Cleaning loop device %s ...", ld)

		if err := ExecCommand("losetup", "-d", ld).Run(); err != nil {
			FlushBlockDeviceBuffers(ld)
			log.Printf("Unable to close loop device %s", ld)
			out, _ := ExecCommand("findmnt", ld).CombinedOutput()
			log.Println(string(out))
			continue
		}
	}
	DevicesSettle()
}

// PathExists returns true if the path exists (file, directory, or other).
func PathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// FileExists returns true if path exists and is a regular file.
func FileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// DirectoryExists returns true if path exists and is a directory.
func DirectoryExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// ListSubmounts returns a list of submounts for a given mountpoint.
func ListSubmounts(mnt string) ([]string, error) {
	if mnt == "" {
		return nil, fmt.Errorf("missing argument")
	}
	cmd := ExecCommand("findmnt", "-rn", "-o", "TARGET", "--submounts", "--target", mnt)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var submounts []string
	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.HasPrefix(line, mnt) {
			submounts = append(submounts, line)
		}
	}
	return submounts, nil
}

// CheckDirNotFsRoot checks if a directory is the root of the filesystem.
func CheckDirNotFsRoot(mnt string) error {
	if mnt == "" {
		return fmt.Errorf("missing mnt parameter")
	}

	rootStat, err := os.Stat("/")
	if err != nil {
		return err
	}
	mntStat, err := os.Stat(mnt)
	if err != nil {
		return err
	}

	if os.SameFile(rootStat, mntStat) {
		return fmt.Errorf("CRITICAL ERROR: %s IS MAPPED TO HOST ROOT. ABORTING", mnt)
	}
	return nil
}

var slaveMounts = []string{
	"/dev",
	"/dev/pts",
	"/sys",
}

// SetupCommonRootfsMounts sets up common rootfs mounts.
func SetupCommonRootfsMounts(mnt string) ([]string, error) {
	if mnt == "" {
		return nil, fmt.Errorf("missing mnt parameter")
	}

	if _, err := os.Stat(mnt); os.IsNotExist(err) {
		return nil, fmt.Errorf("%s does not exist", mnt)
	}
	if err := CheckDirNotFsRoot(mnt); err != nil {
		return nil, err
	}

	var mountsList []string
	for _, d := range slaveMounts {
		dst := filepath.Join(mnt, d)
		if err := os.MkdirAll(dst, 0755); err != nil {
			return nil, err
		}
		if err := sysMount(d, dst, "", unix.MS_BIND, ""); err != nil {
			return nil, fmt.Errorf("failed to bind mount %s: %w", d, err)
		}
		mountsList = append(mountsList, dst)
		if err := sysMount("", dst, "", unix.MS_SLAVE, ""); err != nil {
			return nil, fmt.Errorf("failed to make slave %s: %w", dst, err)
		}
	}

	chrootDevShm := filepath.Join(mnt, "dev", "shm")
	if err := os.MkdirAll(chrootDevShm, 0755); err != nil {
		return nil, err
	}
	if err := sysMount("devshm", chrootDevShm, "tmpfs", unix.MS_NOSUID|unix.MS_NODEV, "mode=1777"); err != nil {
		return nil, fmt.Errorf("failed to mount devshm: %w", err)
	}
	mountsList = append(mountsList, chrootDevShm)

	chrootProc := filepath.Join(mnt, "proc")
	if err := os.MkdirAll(chrootProc, 0755); err != nil {
		return nil, err
	}
	if err := sysMount("proc", chrootProc, "proc", 0, ""); err != nil {
		return nil, fmt.Errorf("failed to mount proc: %w", err)
	}
	mountsList = append(mountsList, chrootProc)

	runLock := filepath.Join(mnt, "run", "lock")
	if err := os.MkdirAll(runLock, 0755); err != nil {
		return nil, err
	}
	if err := sysMount("none", runLock, "tmpfs", unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC|unix.MS_RELATIME, "size=5120k"); err != nil {
		return nil, fmt.Errorf("failed to mount run/lock: %w", err)
	}
	mountsList = append(mountsList, runLock)

	return mountsList, nil
}

// UnsetupCommonRootfsMounts unsets common rootfs mounts.
func UnsetupCommonRootfsMounts(mnt string) error {
	if mnt == "" {
		return fmt.Errorf("missing mnt parameter")
	}

	if _, err := os.Stat(mnt); os.IsNotExist(err) {
		return fmt.Errorf("%s does not exist", mnt)
	}
	if err := CheckDirNotFsRoot(mnt); err != nil {
		return err
	}

	var mounts []string
	for _, d := range slaveMounts {
		mounts = append(mounts, filepath.Join(mnt, d))
	}
	mounts = append(mounts,
		filepath.Join(mnt, "dev", "shm"),
		filepath.Join(mnt, "proc"),
		filepath.Join(mnt, "run", "lock"),
	)
	CleanupMounts(mounts)
	return nil
}

// BindMount binds a source directory to a destination directory.
func BindMount(src, dst string) (string, error) {
	if src == "" {
		return "", fmt.Errorf("missing src parameter")
	}
	if dst == "" {
		return "", fmt.Errorf("missing dst parameter")
	}

	if _, err := os.Stat(src); os.IsNotExist(err) {
		return "", fmt.Errorf("%s does not exist", src)
	}
	if _, err := os.Stat(dst); os.IsNotExist(err) {
		return "", fmt.Errorf("%s does not exist", dst)
	}

	if err := CheckDirNotFsRoot(src); err != nil {
		return "", err
	}
	if err := CheckDirNotFsRoot(dst); err != nil {
		return "", err
	}

	// log.Printf("Binding %s to %s", src, dst)
	if err := sysMount(src, dst, "", unix.MS_BIND, ""); err != nil {
		return "", fmt.Errorf("mount bind failed: %w", err)
	}
	if err := sysMount("", dst, "", unix.MS_SLAVE, ""); err != nil {
		return "", fmt.Errorf("mount make-slave failed: %w", err)
	}
	return dst, nil
}

// BindUmount unmounts a bind mount.
func BindUmount(mnt string) error {
	if mnt == "" {
		return fmt.Errorf("missing mnt parameter")
	}
	if _, err := os.Stat(mnt); os.IsNotExist(err) {
		return fmt.Errorf("%s does not exist", mnt)
	}
	if err := CheckDirNotFsRoot(mnt); err != nil {
		return err
	}
	CleanupMounts([]string{mnt})
	return nil
}

// BindMountDistdir binds the distfiles directory.
func BindMountDistdir(distfilesDir, rootfs string) (string, error) {
	if distfilesDir == "" {
		return "", fmt.Errorf("missing parameter distfilesDir")
	}
	if rootfs == "" {
		return "", fmt.Errorf("missing rootfs parameter")
	}

	if _, err := os.Stat(distfilesDir); os.IsNotExist(err) {
		return "", fmt.Errorf("%s does not exist", distfilesDir)
	}
	if _, err := os.Stat(rootfs); os.IsNotExist(err) {
		return "", fmt.Errorf("%s does not exist", rootfs)
	}

	dstDir := filepath.Join(rootfs, "var", "cache", "distfiles")
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return "", err
	}
	return BindMount(distfilesDir, dstDir)
}

// BindUmountDistdir unmounts the distfiles directory.
func BindUmountDistdir(rootfs string) error {
	if rootfs == "" {
		return fmt.Errorf("missing rootfs parameter")
	}
	if _, err := os.Stat(rootfs); os.IsNotExist(err) {
		return fmt.Errorf("%s does not exist", rootfs)
	}
	dstDir := filepath.Join(rootfs, "var", "cache", "distfiles")
	return BindUmount(dstDir)
}

// BindMountBinpkgs binds the binpkgs directory.
func BindMountBinpkgs(binpkgsDir, rootfs string) (string, error) {
	if binpkgsDir == "" {
		return "", fmt.Errorf("missing parameter binpkgsDir")
	}
	if rootfs == "" {
		return "", fmt.Errorf("missing rootfs parameter")
	}

	if _, err := os.Stat(binpkgsDir); os.IsNotExist(err) {
		return "", fmt.Errorf("%s does not exist", binpkgsDir)
	}
	if _, err := os.Stat(rootfs); os.IsNotExist(err) {
		return "", fmt.Errorf("%s does not exist", rootfs)
	}

	dstDir := filepath.Join(rootfs, "var", "cache", "binpkgs")
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return "", err
	}
	return BindMount(binpkgsDir, dstDir)
}

// BindUmountBinpkgs unmounts the binpkgs directory.
func BindUmountBinpkgs(rootfs string) error {
	if rootfs == "" {
		return fmt.Errorf("missing rootfs parameter")
	}
	if _, err := os.Stat(rootfs); os.IsNotExist(err) {
		return fmt.Errorf("%s does not exist", rootfs)
	}
	dstDir := filepath.Join(rootfs, "var", "cache", "binpkgs")
	return BindUmount(dstDir)
}

// CheckFsCapabilitySupport checks if the filesystem has capability support.
var CheckFsCapabilitySupport = checkFsCapabilitySupport

func checkFsCapabilitySupport(testDir string) (bool, error) {
	if testDir == "" {
		return false, fmt.Errorf("missing parameter testDir")
	}

	tmpBin, err := os.CreateTemp(testDir, ".cap_test.*.bin")
	if err != nil {
		return false, err
	}
	tmpBin.Close()
	defer os.Remove(tmpBin.Name())

	if err := ExecCommand("setcap", "cap_net_raw+ep", tmpBin.Name()).Run(); err != nil {
		log.Println("WARNING: System/FS does not allow setting capabilities.")
		return false, nil
	}

	tmpCopy := tmpBin.Name() + ".copy"
	defer os.Remove(tmpCopy)

	if err := ExecCommand("cp", "-a", tmpBin.Name(), tmpCopy).Run(); err != nil {
		return false, err
	}

	out, err := ExecCommand("getcap", tmpCopy).Output()
	if err != nil {
		// getcap might fail if no caps? No, it just prints nothing usually.
		return false, err
	}

	outStr := string(out)
	// Flexible check for "cap_net_raw+ep" or "cap_net_raw=ep"
	if strings.Contains(outStr, "cap_net_raw+ep") || strings.Contains(outStr, "cap_net_raw=ep") {
		return true, nil
	}
	return false, nil
}

// CheckHardlinkPreservation verifies that hardlinks are preserved between source and destination.
func CheckHardlinkPreservation(src, dst string) error {
	if src == "" || dst == "" {
		return fmt.Errorf("missing parameter (src: %s, dst: %s)", src, dst)
	}
	log.Printf("Checking hardlink preservation from %s to %s...", src, dst)

	// 1. Walk the source directory to find files with multiple links.
	// 2. Track Inodes to find the first pair of files sharing the same inode.
	// Using a map for O(1) checks.

	// Map from Inode (uint64) -> Path
	seenInodes := make(map[uint64]string)

	var file1Src, file2Src string
	foundPair := false

	// Sentinel error to stop walking early
	errFoundPair := fmt.Errorf("found pair")

	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		sys, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return nil
		}

		if sys.Nlink > 1 {
			if existingPath, ok := seenInodes[sys.Ino]; ok {
				file1Src = existingPath
				file2Src = path
				foundPair = true
				return errFoundPair
			}
			seenInodes[sys.Ino] = path
		}
		return nil
	})

	if err != nil && err != errFoundPair {
		return fmt.Errorf("error walking source directory: %w", err)
	}

	if !foundPair {
		log.Println("WARNING: no hardlinked file pairs found in source. Cannot verify.")
		return nil
	}

	relPath1, err := filepath.Rel(src, file1Src)
	if err != nil {
		return err
	}
	relPath2, err := filepath.Rel(src, file2Src)
	if err != nil {
		return err
	}

	file1Dst := filepath.Join(dst, relPath1)
	file2Dst := filepath.Join(dst, relPath2)

	info1, err := os.Stat(file1Dst)
	if err != nil {
		return err
	}
	info2, err := os.Stat(file2Dst)
	if err != nil {
		return err
	}

	stat1, ok1 := info1.Sys().(*syscall.Stat_t)
	stat2, ok2 := info2.Sys().(*syscall.Stat_t)

	if !ok1 || !ok2 {
		return fmt.Errorf("could not get inode info")
	}

	if stat1.Ino != stat2.Ino {
		return fmt.Errorf(
			"CRITICAL: hardlinks BROKEN! Files were duplicated.\n  File 1: %s (inode: %d)\n  File 2: %s (inode: %d)",
			file1Dst, stat1.Ino, file2Dst, stat2.Ino,
		)
	}

	log.Printf("SUCCESS: hardlinks preserved (Inode: %d).", stat1.Ino)
	return nil
}

// CheckDirIsRoot checks if a directory is the root of the filesystem and exits if it is.
func CheckDirIsRoot(chrootDir string) error {
	if chrootDir == "" {
		return fmt.Errorf("missing chrootDir parameter")
	}
	rootStat, err := os.Stat("/")
	if err != nil {
		return err
	}
	chrootStat, err := os.Stat(chrootDir)
	if err != nil {
		return err
	}
	if os.SameFile(rootStat, chrootStat) {
		return fmt.Errorf("CRITICAL ERROR: %s IS MAPPED TO HOST ROOT. ABORTING", chrootDir)
	}
	return nil
}

// CheckDirsSameFilesystem checks if two directories are on the same filesystem.
func CheckDirsSameFilesystem(src, dst string) (bool, error) {
	if src == "" || dst == "" {
		return false, fmt.Errorf("missing parameters src or dst")
	}
	srcStat, err := os.Stat(src)
	if err != nil {
		return false, err
	}
	dstStat, err := os.Stat(dst)
	if err != nil {
		return false, err
	}
	return srcStat.Sys().(*syscall.Stat_t).Dev == dstStat.Sys().(*syscall.Stat_t).Dev, nil
}

// CheckActiveMounts checks for active mounts under a given directory.
func CheckActiveMounts(chrootDir string) error {
	if chrootDir == "" {
		return fmt.Errorf("missing chrootDir parameter")
	}
	out, _ /* ignore any errors */ := ExecCommand(
		"findmnt", "-rn", "-o", "TARGET", "--submounts",
		"--target", chrootDir,
	).Output()

	// we do not expect weird chars in mount points.
	if len(out) == 0 {
		return nil
	}

	var foundMounts []string
	// Check if the output actually contains paths starting with chrootDir
	for bl := range bytes.SplitSeq(out, []byte("\n")) {
		if len(bl) == 0 {
			continue
		}

		bl = bytes.ToValidUTF8(bl, []byte("?"))
		line := string(bl)
		if strings.HasPrefix(line, chrootDir) {
			foundMounts = append(foundMounts, line)
		}
	}

	if len(foundMounts) > 0 {
		return fmt.Errorf(
			"cannot operate sync to %s. Active mounts detected:\n- %s\nPlease umount manually.",
			chrootDir, strings.Join(foundMounts, "\n- "),
		)
	}

	return nil
}

// CpReflinkCopyAllowed checks if a reflink copy is allowed.
func CpReflinkCopyAllowed(src, dst string, useCpFlag bool) (bool, error) {
	if src == "" || dst == "" {
		return false, fmt.Errorf("missing parameters (src: %s, dst: %s)", src, dst)
	}
	if !useCpFlag || src == "/" {
		return false, nil
	}
	sameFs, err := CheckDirsSameFilesystem(src, dst)
	if err != nil {
		return false, err
	}
	if !sameFs {
		return false, nil
	}
	srcCap, err := CheckFsCapabilitySupport(src)
	if err != nil {
		return false, err
	}
	dstCap, err := CheckFsCapabilitySupport(dst)
	if err != nil {
		return false, err
	}
	return srcCap && dstCap, nil
}

// CreateTempDir creates a temporary directory.
func CreateTempDir(parentDir, prefix string) (string, error) {
	if parentDir == "" {
		return "", fmt.Errorf("missing parentDir parameter")
	}
	if prefix == "" {
		prefix = "tmp"
	}
	// os.MkdirTemp is the replacement for ioutil.TempDir
	tempDir, err := os.MkdirTemp(parentDir, prefix)
	if err != nil {
		return "", fmt.Errorf("failed to create temporary directory: %w", err)
	}
	return tempDir, nil
}

// CreateTempFile creates a temporary file.
func CreateTempFile(parentDir, prefix string) (*os.File, error) {
	if parentDir == "" {
		return nil, fmt.Errorf("missing parentDir parameter")
	}
	if prefix == "" {
		prefix = "tmp"
	}
	// os.CreateTemp is the replacement for ioutil.TempFile
	tempFile, err := os.CreateTemp(parentDir, prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary file: %w", err)
	}
	return tempFile, nil
}

// RemoveFileWithGlob removes files matching a glob pattern.
func RemoveFileWithGlob(target string) error {
	matches, err := filepath.Glob(target)
	if err != nil {
		return err
	}
	for _, match := range matches {
		err := os.Remove(match)
		if err != nil {
			return err
		}
	}
	return nil
}

// RemoveDir removes a directory.
func RemoveDir(target string) error {
	if _, err := os.Stat(target); os.IsNotExist(err) {
		log.Printf("Removing: %s does not exist\n", target)
		return nil
	}
	log.Printf("Removing %s\n", target)
	return os.RemoveAll(target)
}

// EmptyDir empties a directory.
func EmptyDir(target string) error {
	if _, err := os.Stat(target); os.IsNotExist(err) {
		log.Printf("Emptying: %s does not exist\n", target)
		return nil
	}
	log.Printf("Emptying directory %s\n", target)

	entries, err := os.ReadDir(target)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(target, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return nil
}

// DirEmpty checks if a directory is empty.
func DirEmpty(dir string) (bool, error) {
	f, err := os.Open(dir)
	if err != nil {
		return false, err
	}
	defer f.Close()

	_, err = f.ReadDir(1)
	if err == io.EOF {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

// ChrootCmd runs a command in a chroot environment using unshare.
func ChrootCmd(chrootDir, chrootExec string, args ...string) (*exec.Cmd, error) {
	if chrootDir == "" {
		return nil, fmt.Errorf("missing chrootDir parameter")
	}
	if chrootExec == "" {
		return nil, fmt.Errorf("missing chrootExec parameter")
	}

	cmdArgs := []string{
		"--pid",
		"--fork",
		"--kill-child",
		"--mount",
		"--uts",
		"--ipc",
		fmt.Sprintf("--mount-proc=%s/proc", chrootDir),
		"chroot",
		chrootDir,
		chrootExec,
	}
	cmdArgs = append(cmdArgs, args...)

	return ExecCommand("unshare", cmdArgs...), nil
}

// Chroot runs a command in a chroot environment using unshare.
func Chroot(chrootDir, chrootExec string, args ...string) error {
	cmd, err := ChrootCmd(chrootDir, chrootExec, args...)
	if err != nil {
		return err
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("chroot failed: %w", err)
	}
	return nil
}
