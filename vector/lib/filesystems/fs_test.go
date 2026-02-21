package filesystems

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func mkPI(path, typ string, perms uint32, uid, gid, size uint64, link string) PathInfo {
	return PathInfo{
		Mode: &PathMode{Type: typ, Perms: os.FileMode(perms)},
		Uid:  uid, Gid: gid, Size: size,
		Path: path, Link: link,
	}
}

func TestPathInfoMetaEqual(t *testing.T) {
	a := mkPI("/usr/etc/foo", "-", 0644, 0, 0, 100, "")
	b := mkPI("/etc/foo", "-", 0644, 0, 0, 100, "")
	if !a.Equals(&b) {
		t.Error("Expected equal (path is not compared)")
	}

	// Different perms
	c := mkPI("/etc/foo", "-", 0755, 0, 0, 100, "")
	if a.Equals(&c) {
		t.Error("Expected not equal (different perms)")
	}

	// Different size
	d := mkPI("/etc/foo", "-", 0644, 0, 0, 200, "")
	if a.Equals(&d) {
		t.Error("Expected not equal (different size)")
	}

	// Different type
	e := mkPI("/etc/foo", "l", 0644, 0, 0, 100, "/bar")
	if a.Equals(&e) {
		t.Error("Expected not equal (different type)")
	}

	// Symlinks with different targets
	f := mkPI("/etc/link", "l", 0777, 0, 0, 0, "target_a")
	g := mkPI("/etc/link", "l", 0777, 0, 0, 0, "target_b")
	if f.Equals(&g) {
		t.Error("Expected not equal (different link target)")
	}
}

// fakeExecCommand mocks exec.Command for testing purposes.
func fakeExecCommand(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
	// Pass through specific environment variables for controlling mock behavior
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "MOCK_") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	return cmd
}

// fakeExecRun wraps fakeExecCommand to implement runner.Func.
func fakeExecRun(stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
	cmd := fakeExecCommand(name, args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// fakeExecOutput wraps fakeExecCommand to implement runner.OutputFunc.
func fakeExecOutput(name string, args ...string) ([]byte, error) {
	return fakeExecCommand(name, args...).Output()
}

// fakeExecCombinedOutput wraps fakeExecCommand to implement runner.CombinedOutputFunc.
func fakeExecCombinedOutput(name string, args ...string) ([]byte, error) {
	return fakeExecCommand(name, args...).CombinedOutput()
}

// fakeChrootRun wraps fakeExecCommand to implement runner.ChrootRunFunc.
func fakeChrootRun(stdin io.Reader, stdout, stderr io.Writer, chrootDir, chrootExec string, args ...string) error {
	if chrootDir == "" {
		return fmt.Errorf("missing chrootDir parameter")
	}
	if chrootExec == "" {
		return fmt.Errorf("missing chrootExec parameter")
	}
	// Build the same unshare args that runner.chrootArgs would build,
	// then delegate to fakeExecRun so TestHelperProcess handles "unshare".
	cmdArgs := []string{
		"--pid", "--fork", "--kill-child", "--mount", "--uts", "--ipc",
		fmt.Sprintf("--mount-proc=%s/proc", chrootDir),
		"chroot", chrootDir, chrootExec,
	}
	cmdArgs = append(cmdArgs, args...)
	return fakeExecRun(stdin, stdout, stderr, "unshare", cmdArgs...)
}

// fakeChrootOutput wraps fakeExecCommand to implement runner.ChrootOutputFunc.
func fakeChrootOutput(chrootDir, chrootExec string, args ...string) ([]byte, error) {
	if chrootDir == "" {
		return nil, fmt.Errorf("missing chrootDir parameter")
	}
	if chrootExec == "" {
		return nil, fmt.Errorf("missing chrootExec parameter")
	}
	cmdArgs := []string{
		"--pid", "--fork", "--kill-child", "--mount", "--uts", "--ipc",
		fmt.Sprintf("--mount-proc=%s/proc", chrootDir),
		"chroot", chrootDir, chrootExec,
	}
	cmdArgs = append(cmdArgs, args...)
	return fakeExecOutput("unshare", cmdArgs...)
}

// setupMockExec swaps all execution vars with fakes and registers cleanup.
func setupMockExec(t *testing.T) {
	origExecRun := execRun
	origExecOutput := execOutput
	origExecCombinedOutput := execCombinedOutput
	origChrootRun := ExecChrootRun
	origChrootOutput := ExecChrootOutput

	execRun = fakeExecRun
	execOutput = fakeExecOutput
	execCombinedOutput = fakeExecCombinedOutput
	ExecChrootRun = fakeChrootRun
	ExecChrootOutput = fakeChrootOutput

	t.Cleanup(func() {
		execRun = origExecRun
		execOutput = origExecOutput
		execCombinedOutput = origExecCombinedOutput
		ExecChrootRun = origChrootRun
		ExecChrootOutput = origChrootOutput
	})
}

func setupMockSyscalls(t *testing.T) {
	origMount := sysMount
	origUnmount := sysUnmount
	origIoctl := sysIoctl

	sysMount = func(source string, target string, fstype string, flags uintptr, data string) error {
		if os.Getenv("MOCK_MOUNT_FAIL") == "1" {
			return fmt.Errorf("mock mount failed")
		}
		return nil
	}
	sysUnmount = func(target string, flags int) error {
		if os.Getenv("MOCK_UMOUNT_FAIL") == "1" {
			return fmt.Errorf("mock unmount failed")
		}
		return nil
	}
	sysIoctl = func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err unix.Errno) {
		return 0, 0, 0
	}

	t.Cleanup(func() {
		sysMount = origMount
		sysUnmount = origUnmount
		sysIoctl = origIoctl
	})
}

// TestHelperProcess is the mock process that runs instead of the real commands.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "No command\n")
		os.Exit(2)
	}

	cmd, args := args[0], args[1:]
	switch cmd {
	case "cryptsetup":
		if os.Getenv("MOCK_CRYPTSETUP_FAIL") == "1" {
			fmt.Fprintln(os.Stderr, "cryptsetup failed")
			os.Exit(1)
		}
	case "losetup":
		if val := os.Getenv("MOCK_LOSETUP_OUTPUT"); val != "" {
			fmt.Fprint(os.Stdout, val)
		}
		if os.Getenv("MOCK_LOSETUP_FAIL") == "1" {
			os.Exit(1)
		}
	case "setcap":
		// Success
	case "cp":
		// cp src dst
		if len(args) >= 2 {
			src := args[len(args)-2]
			dst := args[len(args)-1]
			data, _ := os.ReadFile(src)
			os.WriteFile(dst, data, 0644)
		}
	case "getcap":
		fmt.Println("/path/to/file = cap_net_raw+ep")
	case "udevadm", "blockdev":
		// No-op success
	case "unshare":
		if os.Getenv("MOCK_UNSHARE_FAIL") == "1" {
			fmt.Fprintln(os.Stderr, "unshare failed")
			os.Exit(1)
		}
	default:
		// Pass for other commands
	}
	os.Exit(0)
}

func TestDeviceUUID(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		uuidDir, _ := setupMockDevDisk(t)
		expectedUUID := "1234-5678"
		devFile := filepath.Join(t.TempDir(), "sda1")
		if err := os.WriteFile(devFile, nil, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(devFile, filepath.Join(uuidDir, expectedUUID)); err != nil {
			t.Fatal(err)
		}

		uuid, err := DeviceUUID(devFile)
		if err != nil {
			t.Fatalf("DeviceUUID failed: %v", err)
		}
		if uuid != expectedUUID {
			t.Errorf("Expected UUID %s, got %s", expectedUUID, uuid)
		}
	})

	t.Run("NoDevPath", func(t *testing.T) {
		_, err := DeviceUUID("")
		if err == nil {
			t.Error("Expected error for missing devPath, got nil")
		}
	})

	t.Run("DeviceNotFound", func(t *testing.T) {
		setupMockDevDisk(t)
		devFile := filepath.Join(t.TempDir(), "sda1")
		if err := os.WriteFile(devFile, nil, 0644); err != nil {
			t.Fatal(err)
		}
		_, err := DeviceUUID(devFile)
		if err == nil {
			t.Error("Expected error for device not found in by-uuid, got nil")
		}
	})

	t.Run("NonexistentDevice", func(t *testing.T) {
		setupMockDevDisk(t)
		_, err := DeviceUUID("/dev/nonexistent_device_xyz")
		if err == nil {
			t.Error("Expected error for nonexistent device path, got nil")
		}
	})
}

func TestDevicePartUUID(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		_, partuuidDir := setupMockDevDisk(t)
		expectedPartUUID := "abcdef-01"
		devFile := filepath.Join(t.TempDir(), "sda1")
		if err := os.WriteFile(devFile, nil, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(devFile, filepath.Join(partuuidDir, expectedPartUUID)); err != nil {
			t.Fatal(err)
		}

		partuuid, err := DevicePartUUID(devFile)
		if err != nil {
			t.Fatalf("DevicePartUUID failed: %v", err)
		}
		if partuuid != expectedPartUUID {
			t.Errorf("Expected PARTUUID %s, got %s", expectedPartUUID, partuuid)
		}
	})

	t.Run("NoDevPath", func(t *testing.T) {
		_, err := DevicePartUUID("")
		if err == nil {
			t.Error("Expected error for missing devPath, got nil")
		}
	})

	t.Run("DeviceNotFound", func(t *testing.T) {
		setupMockDevDisk(t)
		devFile := filepath.Join(t.TempDir(), "sda1")
		if err := os.WriteFile(devFile, nil, 0644); err != nil {
			t.Fatal(err)
		}
		_, err := DevicePartUUID(devFile)
		if err == nil {
			t.Error("Expected error for device not found in by-partuuid, got nil")
		}
	})

	t.Run("NonexistentDevice", func(t *testing.T) {
		setupMockDevDisk(t)
		_, err := DevicePartUUID("/dev/nonexistent_device_xyz")
		if err == nil {
			t.Error("Expected error for nonexistent device path, got nil")
		}
	})
}

func TestMountpointToDevice(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		setupMockMountInfo(t, []*MountInfoEntry{
			{Mountpoint: "/mnt", Source: "/dev/sda1", FSType: "ext4"},
		})

		device, err := MountpointToDevice("/mnt")
		if err != nil {
			t.Fatalf("MountpointToDevice failed: %v", err)
		}
		if device != "/dev/sda1" {
			t.Errorf("Expected device /dev/sda1, got %s", device)
		}
	})

	t.Run("NoMountpoint", func(t *testing.T) {
		_, err := MountpointToDevice("")
		if err == nil {
			t.Error("Expected error for missing mountpoint, got nil")
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		setupMockMountInfo(t, []*MountInfoEntry{})
		_, err := MountpointToDevice("/mnt")
		if err == nil {
			t.Error("Expected error when mount not found")
		}
	})

	t.Run("MultipleOutputs", func(t *testing.T) {
		setupMockMountInfo(t, []*MountInfoEntry{
			{Mountpoint: "/mnt", Source: "/dev/sda1", FSType: "ext4"},
			{Mountpoint: "/mnt", Source: "/dev/sda2", FSType: "ext4"},
		})
		device, err := MountpointToDevice("/mnt")
		if err != nil {
			t.Fatalf("MountpointToDevice failed: %v", err)
		}
		if device != "/dev/sda2" {
			t.Errorf("Expected most recent device /dev/sda2, got %s", device)
		}
	})
}

func TestMountpointToUUID(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		uuidDir, _ := setupMockDevDisk(t)
		devFile := filepath.Join(t.TempDir(), "sda1")
		if err := os.WriteFile(devFile, nil, 0644); err != nil {
			t.Fatal(err)
		}
		expectedUUID := "abcd-1234-ef56-7890"
		if err := os.Symlink(devFile, filepath.Join(uuidDir, expectedUUID)); err != nil {
			t.Fatal(err)
		}
		setupMockMountInfo(t, []*MountInfoEntry{
			{Mountpoint: "/mnt", Source: devFile, FSType: "ext4"},
		})

		uuid, err := MountpointToUUID("/mnt")
		if err != nil {
			t.Fatalf("MountpointToUUID failed: %v", err)
		}
		if uuid != expectedUUID {
			t.Errorf("Expected UUID %s, got %s", expectedUUID, uuid)
		}
	})

	t.Run("NoMountpoint", func(t *testing.T) {
		_, err := MountpointToUUID("")
		if err == nil {
			t.Error("Expected error for missing mountpoint, got nil")
		}
	})

	t.Run("MountNotFound", func(t *testing.T) {
		setupMockMountInfo(t, []*MountInfoEntry{})
		_, err := MountpointToUUID("/mnt")
		if err == nil {
			t.Error("Expected error when mount not found")
		}
	})

	t.Run("NoUUID", func(t *testing.T) {
		setupMockDevDisk(t)
		setupMockMountInfo(t, []*MountInfoEntry{
			{Mountpoint: "/mnt", Source: "tmpfs", FSType: "tmpfs"},
		})
		_, err := MountpointToUUID("/mnt")
		if err == nil {
			t.Error("Expected error for no UUID found, got nil")
		}
	})
}

func TestMountpointToFSType(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		setupMockMountInfo(t, []*MountInfoEntry{
			{Mountpoint: "/mnt", Source: "/dev/sda1", FSType: "ext4"},
		})

		fstype, err := MountpointToFSType("/mnt")
		if err != nil {
			t.Fatalf("MountpointToFSType failed: %v", err)
		}
		if fstype != "ext4" {
			t.Errorf("Expected FSTYPE ext4, got %s", fstype)
		}
	})

	t.Run("SuccessVfat", func(t *testing.T) {
		setupMockMountInfo(t, []*MountInfoEntry{
			{Mountpoint: "/boot/efi", Source: "/dev/sda2", FSType: "vfat"},
		})

		fstype, err := MountpointToFSType("/boot/efi")
		if err != nil {
			t.Fatalf("MountpointToFSType failed: %v", err)
		}
		if fstype != "vfat" {
			t.Errorf("Expected FSTYPE vfat, got %s", fstype)
		}
	})

	t.Run("NoMountpoint", func(t *testing.T) {
		_, err := MountpointToFSType("")
		if err == nil {
			t.Error("Expected error for missing mountpoint, got nil")
		}
	})

	t.Run("MountNotFound", func(t *testing.T) {
		setupMockMountInfo(t, []*MountInfoEntry{})
		_, err := MountpointToFSType("/mnt")
		if err == nil {
			t.Error("Expected error when mount not found")
		}
	})
}

func TestListSubmounts(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		setupMockMountInfo(t, []*MountInfoEntry{
			{Mountpoint: "/mnt/test"},
			{Mountpoint: "/mnt/test/sub"},
		})

		submounts, err := ListSubmounts("/mnt/test")
		if err != nil {
			t.Fatalf("ListSubmounts failed: %v", err)
		}
		if len(submounts) != 2 {
			t.Errorf("Expected 2 submounts, got %d", len(submounts))
		}
	})

	t.Run("NoMountpoint", func(t *testing.T) {
		_, err := ListSubmounts("")
		if err == nil {
			t.Error("Expected error for missing mountpoint, got nil")
		}
	})

	t.Run("ReadFail", func(t *testing.T) {
		setupMockMountInfoFail(t)
		_, err := ListSubmounts("/mnt")
		if err == nil {
			t.Error("Expected error from mountinfo read failure")
		}
	})
}

func TestGetLuksRootfsDevicePath(t *testing.T) {
	tests := []struct {
		name     string
		luksName string
		want     string
		wantErr  bool
	}{
		{"Valid", "mycrypt", "/dev/mapper/mycrypt", false},
		{"Empty", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetLuksRootfsDevicePath(tt.luksName)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetLuksRootfsDevicePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GetLuksRootfsDevicePath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTempDirAndFileOperations(t *testing.T) {
	tmpDir, err := CreateTempDir(os.TempDir(), "test-fs-")
	if err != nil {
		t.Fatalf("CreateTempDir failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	info, err := os.Stat(tmpDir)
	if err != nil || !info.IsDir() {
		t.Errorf("CreateTempDir did not create a directory")
	}

	tmpFile, err := CreateTempFile(tmpDir, "test-file-")
	if err != nil {
		t.Fatalf("CreateTempFile failed: %v", err)
	}
	tmpFile.Close()

	if _, err := os.Stat(tmpFile.Name()); os.IsNotExist(err) {
		t.Errorf("CreateTempFile did not create a file")
	}

	isEmpty, err := DirEmpty(tmpDir)
	if err != nil {
		t.Errorf("DirEmpty failed: %v", err)
	}
	if isEmpty {
		t.Errorf("DirEmpty returned true for non-empty dir")
	}

	pattern := filepath.Join(tmpDir, "test-file-*")
	if err := RemoveFileWithGlob(pattern); err != nil {
		t.Errorf("RemoveFileWithGlob failed: %v", err)
	}

	if _, err := os.Stat(tmpFile.Name()); !os.IsNotExist(err) {
		t.Errorf("RemoveFileWithGlob did not remove the file")
	}

	isEmpty, err = DirEmpty(tmpDir)
	if err != nil {
		t.Errorf("DirEmpty failed: %v", err)
	}
	if !isEmpty {
		t.Errorf("DirEmpty returned false for empty dir")
	}

	if _, err := os.Create(filepath.Join(tmpDir, "another")); err != nil {
		t.Fatal(err)
	}
	if err := EmptyDir(tmpDir); err != nil {
		t.Errorf("EmptyDir failed: %v", err)
	}
	isEmpty, _ = DirEmpty(tmpDir)
	if !isEmpty {
		t.Errorf("EmptyDir did not empty the directory")
	}

	if err := RemoveDir(tmpDir); err != nil {
		t.Errorf("RemoveDir failed: %v", err)
	}
	if _, err := os.Stat(tmpDir); !os.IsNotExist(err) {
		t.Errorf("RemoveDir did not remove the directory")
	}
}

func TestCheckDirsSameFilesystem(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	same, err := CheckDirsSameFilesystem(tmpDir, subDir)
	if err != nil {
		t.Fatalf("CheckDirsSameFilesystem failed: %v", err)
	}
	if !same {
		t.Errorf("Expected same filesystem for parent and subdir")
	}
}

func TestCheckDirNotFsRoot(t *testing.T) {
	err := CheckDirNotFsRoot("/")
	if err == nil {
		t.Error("CheckDirNotFsRoot(/) should fail")
	}

	tmpDir := t.TempDir()
	err = CheckDirNotFsRoot(tmpDir)
	if err != nil {
		t.Errorf("CheckDirNotFsRoot(tmpDir) failed: %v", err)
	}
}

func TestCheckDirIsRoot(t *testing.T) {
	err := CheckDirIsRoot("/")
	if err == nil {
		t.Error("CheckDirIsRoot(/) should fail")
	}

	tmpDir := t.TempDir()
	err = CheckDirIsRoot(tmpDir)
	if err != nil {
		t.Errorf("CheckDirIsRoot(tmpDir) failed: %v", err)
	}
}

func TestCheckHardlinkPreservation(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	file1 := filepath.Join(srcDir, "file1")
	if err := os.WriteFile(file1, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	file2 := filepath.Join(srcDir, "file2")
	if err := os.Link(file1, file2); err != nil {
		t.Fatal(err)
	}

	dstFile1 := filepath.Join(dstDir, "file1")
	dstFile2 := filepath.Join(dstDir, "file2")
	if err := os.WriteFile(dstFile1, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(dstFile1, dstFile2); err != nil {
		t.Fatal(err)
	}

	if err := CheckHardlinkPreservation(srcDir, dstDir); err != nil {
		t.Errorf("CheckHardlinkPreservation failed when links preserved: %v", err)
	}

	dstDirBroken := t.TempDir()
	dstBroken1 := filepath.Join(dstDirBroken, "file1")
	dstBroken2 := filepath.Join(dstDirBroken, "file2")
	if err := os.WriteFile(dstBroken1, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dstBroken2, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CheckHardlinkPreservation(srcDir, dstDirBroken); err == nil {
		t.Error("CheckHardlinkPreservation should fail when links are broken")
	}
}

func TestCleanupMounts(t *testing.T) {
	setupMockExec(t)
	setupMockSyscalls(t)

	t.Run("SuccessfulUnmount", func(t *testing.T) {
		setupMockMountInfo(t, []*MountInfoEntry{
			{Mountpoint: "/mnt/test", Source: "/dev/sda1"},
		})
		CleanupMounts([]string{"/mnt/test"})
	})

	t.Run("MountNotExist", func(t *testing.T) {
		setupMockMountInfo(t, []*MountInfoEntry{})
		CleanupMounts([]string{"/mnt/test"})
	})

	t.Run("UnmountFail", func(t *testing.T) {
		setupMockMountInfo(t, []*MountInfoEntry{
			{Mountpoint: "/mnt/fail", Source: "/dev/sda1"},
		})
		os.Setenv("MOCK_UMOUNT_FAIL", "1")
		defer os.Unsetenv("MOCK_UMOUNT_FAIL")
		// Should not panic or error out, just log
		CleanupMounts([]string{"/mnt/fail"})
	})
}

func TestSetupCommonRootfsMounts(t *testing.T) {
	setupMockExec(t)
	setupMockSyscalls(t)

	tmpDir := t.TempDir()
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		t.Fatal(err)
	}

	mounts, err := SetupCommonRootfsMounts(tmpDir)
	if err != nil {
		t.Errorf("SetupCommonRootfsMounts failed: %v", err)
	}
	if len(mounts) != 6 {
		t.Errorf("Expected 6 mounts, got %d", len(mounts))
	}
}

func TestBindMount(t *testing.T) {
	setupMockExec(t)
	setupMockSyscalls(t)

	src := t.TempDir()
	dst := t.TempDir()

	if _, err := BindMount(src, dst); err != nil {
		t.Errorf("BindMount failed: %v", err)
	}
}

func TestCleanupLoopDevices(t *testing.T) {
	setupMockExec(t)

	f, err := os.CreateTemp("", "loop")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	os.Setenv("MOCK_LOSETUP_OUTPUT", "/path/to/backing/file")
	defer os.Unsetenv("MOCK_LOSETUP_OUTPUT")

	CleanupLoopDevices([]string{f.Name()})
}

func TestCheckFsCapabilitySupport(t *testing.T) {
	setupMockExec(t)

	tmpDir := t.TempDir()
	supported, err := CheckFsCapabilitySupport(tmpDir)
	if err != nil {
		t.Errorf("CheckFsCapabilitySupport failed: %v", err)
	}
	if !supported {
		t.Error("Expected capability support to be true with mock")
	}
}

func TestCheckActiveMounts(t *testing.T) {
	t.Run("NoActiveMounts", func(t *testing.T) {
		setupMockMountInfo(t, []*MountInfoEntry{})
		if err := CheckActiveMounts("/mnt/test"); err != nil {
			t.Errorf("CheckActiveMounts failed: %v", err)
		}
	})

	t.Run("ActiveMountsDetected", func(t *testing.T) {
		setupMockMountInfo(t, []*MountInfoEntry{
			{Mountpoint: "/mnt/test/proc", Source: "proc", FSType: "proc"},
		})
		if err := CheckActiveMounts("/mnt/test"); err == nil {
			t.Error("CheckActiveMounts should fail when mounts are detected")
		}
	})
}

func TestDevicesSettle(t *testing.T) {
	setupMockExec(t)
	// Simple execution test to ensure it runs without error
	DevicesSettle()
}

func TestFlushBlockDeviceBuffers(t *testing.T) {
	setupMockExec(t)
	setupMockSyscalls(t)

	t.Run("Success", func(t *testing.T) {
		f, err := os.CreateTemp(t.TempDir(), "blockdev")
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
		if err := FlushBlockDeviceBuffers(f.Name()); err != nil {
			t.Errorf("FlushBlockDeviceBuffers failed: %v", err)
		}
	})

	t.Run("NoDevPath", func(t *testing.T) {
		if err := FlushBlockDeviceBuffers(""); err == nil {
			t.Error("Expected error for missing devPath, got nil")
		}
	})
}

func TestUnsetupCommonRootfsMounts(t *testing.T) {
	setupMockExec(t)
	setupMockSyscalls(t)

	t.Run("Success", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Mock that the mounts exist
		setupMockMountInfo(t, []*MountInfoEntry{
			{Mountpoint: filepath.Join(tmpDir, "dev")},
			{Mountpoint: filepath.Join(tmpDir, "dev", "pts")},
			{Mountpoint: filepath.Join(tmpDir, "sys")},
			{Mountpoint: filepath.Join(tmpDir, "dev", "shm")},
			{Mountpoint: filepath.Join(tmpDir, "proc")},
			{Mountpoint: filepath.Join(tmpDir, "run", "lock")},
		})
		if err := UnsetupCommonRootfsMounts(tmpDir); err != nil {
			t.Errorf("UnsetupCommonRootfsMounts failed: %v", err)
		}
	})

	t.Run("MissingMnt", func(t *testing.T) {
		if err := UnsetupCommonRootfsMounts(""); err == nil {
			t.Error("Expected error for missing mnt, got nil")
		}
	})

	t.Run("NonExistentMnt", func(t *testing.T) {
		if err := UnsetupCommonRootfsMounts("/non/existent/path"); err == nil {
			t.Error("Expected error for non-existent mnt, got nil")
		}
	})
}

func TestBindUmount(t *testing.T) {
	setupMockExec(t)
	setupMockSyscalls(t)

	t.Run("Success", func(t *testing.T) {
		tmpDir := t.TempDir()
		setupMockMountInfo(t, []*MountInfoEntry{
			{Mountpoint: tmpDir},
		})
		if err := BindUmount(tmpDir); err != nil {
			t.Errorf("BindUmount failed: %v", err)
		}
	})

	t.Run("MissingMnt", func(t *testing.T) {
		if err := BindUmount(""); err == nil {
			t.Error("Expected error for missing mnt, got nil")
		}
	})

	t.Run("NonExistentMnt", func(t *testing.T) {
		if err := BindUmount("/non/existent/path"); err == nil {
			t.Error("Expected error for non-existent mnt, got nil")
		}
	})
}

func TestBindMountDistdir(t *testing.T) {
	setupMockExec(t)
	setupMockSyscalls(t)

	t.Run("Success", func(t *testing.T) {
		distfilesDir := t.TempDir()
		rootfs := t.TempDir()
		if _, err := BindMountDistdir(distfilesDir, rootfs); err != nil {
			t.Errorf("BindMountDistdir failed: %v", err)
		}
	})
}

func TestBindUmountDistdir(t *testing.T) {
	setupMockExec(t)
	setupMockSyscalls(t)

	t.Run("Success", func(t *testing.T) {
		rootfs := t.TempDir()
		distfilesDir := filepath.Join(rootfs, "var", "cache", "distfiles")
		if err := os.MkdirAll(distfilesDir, 0755); err != nil {
			t.Fatal(err)
		}
		setupMockMountInfo(t, []*MountInfoEntry{
			{Mountpoint: distfilesDir},
		})

		if err := BindUmountDistdir(rootfs); err != nil {
			t.Errorf("BindUmountDistdir failed: %v", err)
		}
	})
}

func TestBindMountBinpkgs(t *testing.T) {
	setupMockExec(t)
	setupMockSyscalls(t)

	t.Run("Success", func(t *testing.T) {
		binpkgsDir := t.TempDir()
		rootfs := t.TempDir()
		if _, err := BindMountBinpkgs(binpkgsDir, rootfs); err != nil {
			t.Errorf("BindMountBinpkgs failed: %v", err)
		}
	})
}

func TestBindUmountBinpkgs(t *testing.T) {
	setupMockExec(t)
	setupMockSyscalls(t)

	t.Run("Success", func(t *testing.T) {
		rootfs := t.TempDir()
		binpkgsDir := filepath.Join(rootfs, "var", "cache", "binpkgs")
		if err := os.MkdirAll(binpkgsDir, 0755); err != nil {
			t.Fatal(err)
		}
		setupMockMountInfo(t, []*MountInfoEntry{
			{Mountpoint: binpkgsDir},
		})

		if err := BindUmountBinpkgs(rootfs); err != nil {
			t.Errorf("BindUmountBinpkgs failed: %v", err)
		}
	})
}

func TestCleanupCryptsetupDevices(t *testing.T) {
	setupMockExec(t)

	t.Run("Success", func(t *testing.T) {
		tmpDir := t.TempDir()

		originalDevMapperPrefix := devMapperPrefix
		t.Cleanup(func() { devMapperPrefix = originalDevMapperPrefix })
		devMapperPrefix = tmpDir

		devPath := filepath.Join(tmpDir, "mycrypt")
		if _, err := os.Create(devPath); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(devPath)
		CleanupCryptsetupDevices([]string{"mycrypt"})
	})

	t.Run("CloseFail", func(t *testing.T) {
		setupMockMountInfo(t, []*MountInfoEntry{})
		os.Setenv("MOCK_CRYPTSETUP_FAIL", "1")
		defer os.Unsetenv("MOCK_CRYPTSETUP_FAIL")
		tmpDir := t.TempDir()

		originalDevMapperPrefix := devMapperPrefix
		t.Cleanup(func() { devMapperPrefix = originalDevMapperPrefix })
		devMapperPrefix = tmpDir

		devPath := filepath.Join(tmpDir, "mycrypt")
		if _, err := os.Create(devPath); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(devPath)

		// Should not panic or error out, just log
		CleanupCryptsetupDevices([]string{"mycrypt"})
	})
}

func TestCpReflinkCopyAllowed(t *testing.T) {
	setupMockExec(t)

	src := t.TempDir()
	dst := t.TempDir()

	t.Run("Allowed", func(t *testing.T) {
		// Mock capability support
		originalCheckFsCapabilitySupport := CheckFsCapabilitySupport
		CheckFsCapabilitySupport = func(testDir string) (bool, error) {
			return true, nil
		}
		t.Cleanup(func() { CheckFsCapabilitySupport = originalCheckFsCapabilitySupport })

		allowed, err := CpReflinkCopyAllowed(src, dst, true)
		if err != nil {
			t.Fatalf("CpReflinkCopyAllowed failed: %v", err)
		}
		if !allowed {
			t.Error("Expected reflink copy to be allowed")
		}
	})

	t.Run("NotAllowedWithoutFlag", func(t *testing.T) {
		allowed, err := CpReflinkCopyAllowed(src, dst, false)
		if err != nil {
			t.Fatalf("CpReflinkCopyAllowed failed: %v", err)
		}
		if allowed {
			t.Error("Expected reflink copy to be not allowed without useCpFlag")
		}
	})

	t.Run("NotAllowedOnRoot", func(t *testing.T) {
		allowed, err := CpReflinkCopyAllowed("/", dst, true)
		if err != nil {
			t.Fatalf("CpReflinkCopyAllowed failed: %v", err)
		}
		if allowed {
			t.Error("Expected reflink copy to be not allowed on root")
		}

	})
}

func TestChrootOutput(t *testing.T) {
	setupMockExec(t)

	t.Run("Success", func(t *testing.T) {
		out, err := ChrootOutput("/target", "/bin/sh", "-c", "echo hello")
		if err != nil {
			t.Fatalf("ChrootOutput failed: %v", err)
		}
		// The mock unshare handler exits 0 with no output by default
		_ = out
	})

	t.Run("MissingChrootDir", func(t *testing.T) {
		_, err := ChrootOutput("", "/bin/sh")
		if err == nil {
			t.Error("Expected error for missing chrootDir, got nil")
		}
	})

	t.Run("MissingChrootExec", func(t *testing.T) {
		_, err := ChrootOutput("/target", "")
		if err == nil {
			t.Error("Expected error for missing chrootExec, got nil")
		}
	})
}

func TestChrootRun(t *testing.T) {
	setupMockExec(t)

	t.Run("Success", func(t *testing.T) {
		if err := ChrootRun("/target", "/bin/true"); err != nil {
			t.Errorf("ChrootRun failed: %v", err)
		}
	})

	t.Run("CommandFail", func(t *testing.T) {
		os.Setenv("MOCK_UNSHARE_FAIL", "1")
		defer os.Unsetenv("MOCK_UNSHARE_FAIL")

		if err := ChrootRun("/target", "/bin/false"); err == nil {
			t.Error("Expected error from unshare failure, got nil")
		}
	})

	t.Run("MissingArgs", func(t *testing.T) {
		if err := ChrootRun("", "/bin/true"); err == nil {
			t.Error("Expected error for missing chrootDir, got nil")
		}
	})
}

func TestListContents(t *testing.T) {
	t.Run("EmptyPath", func(t *testing.T) {
		_, err := ListContents("")
		if err == nil {
			t.Fatal("Expected error for empty path, got nil")
		}
	})

	t.Run("NonExistentPath", func(t *testing.T) {
		_, err := ListContents("/nonexistent/path/that/does/not/exist")
		if err == nil {
			t.Fatal("Expected error for non-existent path, got nil")
		}
	})

	t.Run("EmptyDirectory", func(t *testing.T) {
		dir := t.TempDir()

		pis, err := ListContents(dir)
		if err != nil {
			t.Fatalf("ListContents failed: %v", err)
		}
		// Should contain only the root directory itself
		if len(pis) != 1 {
			t.Fatalf("Expected 1 entry (root dir), got %d", len(pis))
		}
		if pis[0].Mode.Type != "d" {
			t.Errorf("Expected type 'd', got %q", pis[0].Mode.Type)
		}
		if pis[0].Path != dir {
			t.Errorf("Expected path %q, got %q", dir, pis[0].Path)
		}
	})

	t.Run("RegularFiles", func(t *testing.T) {
		dir := t.TempDir()

		content := []byte("hello world")
		filePath := filepath.Join(dir, "file.txt")
		if err := os.WriteFile(filePath, content, 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		pis, err := ListContents(dir)
		if err != nil {
			t.Fatalf("ListContents failed: %v", err)
		}

		// root dir + file.txt
		if len(pis) != 2 {
			t.Fatalf("Expected 2 entries, got %d", len(pis))
		}

		// Find the file entry
		var filePi *PathInfo
		for _, pi := range pis {
			if pi.Path == filePath {
				filePi = pi
				break
			}
		}
		if filePi == nil {
			t.Fatal("File entry not found in results")
		}
		if filePi.Mode.Type != "-" {
			t.Errorf("Expected type '-', got %q", filePi.Mode.Type)
		}
		if filePi.Size != uint64(len(content)) {
			t.Errorf("Expected size %d, got %d", len(content), filePi.Size)
		}
	})

	t.Run("Subdirectories", func(t *testing.T) {
		dir := t.TempDir()

		subdir := filepath.Join(dir, "subdir")
		if err := os.Mkdir(subdir, 0755); err != nil {
			t.Fatalf("Mkdir failed: %v", err)
		}
		nestedFile := filepath.Join(subdir, "nested.txt")
		if err := os.WriteFile(nestedFile, []byte("nested"), 0600); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		pis, err := ListContents(dir)
		if err != nil {
			t.Fatalf("ListContents failed: %v", err)
		}

		// root dir + subdir + nested.txt
		if len(pis) != 3 {
			t.Fatalf("Expected 3 entries, got %d", len(pis))
		}

		pathSet := make(map[string]string) // path -> type
		for _, pi := range pis {
			pathSet[pi.Path] = pi.Mode.Type
		}
		if typ, ok := pathSet[dir]; !ok || typ != "d" {
			t.Errorf("Root dir missing or wrong type: ok=%v type=%q", ok, typ)
		}
		if typ, ok := pathSet[subdir]; !ok || typ != "d" {
			t.Errorf("Subdir missing or wrong type: ok=%v type=%q", ok, typ)
		}
		if typ, ok := pathSet[nestedFile]; !ok || typ != "-" {
			t.Errorf("Nested file missing or wrong type: ok=%v type=%q", ok, typ)
		}
	})

	t.Run("Symlinks", func(t *testing.T) {
		dir := t.TempDir()

		target := filepath.Join(dir, "target.txt")
		if err := os.WriteFile(target, []byte("target"), 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}
		link := filepath.Join(dir, "link.txt")
		if err := os.Symlink("target.txt", link); err != nil {
			t.Fatalf("Symlink failed: %v", err)
		}

		pis, err := ListContents(dir)
		if err != nil {
			t.Fatalf("ListContents failed: %v", err)
		}

		// root dir + target.txt + link.txt
		if len(pis) != 3 {
			t.Fatalf("Expected 3 entries, got %d", len(pis))
		}

		var linkPi *PathInfo
		for _, pi := range pis {
			if pi.Path == link {
				linkPi = pi
				break
			}
		}
		if linkPi == nil {
			t.Fatal("Symlink entry not found in results")
		}
		if linkPi.Mode.Type != "l" {
			t.Errorf("Expected type 'l', got %q", linkPi.Mode.Type)
		}
		if linkPi.Link != "target.txt" {
			t.Errorf("Expected link target 'target.txt', got %q", linkPi.Link)
		}
	})

	t.Run("SpecialFilesIgnored", func(t *testing.T) {
		dir := t.TempDir()

		// Create a regular file and a FIFO (named pipe)
		regFile := filepath.Join(dir, "regular.txt")
		if err := os.WriteFile(regFile, []byte("data"), 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}
		fifoPath := filepath.Join(dir, "myfifo")
		if err := unix.Mkfifo(fifoPath, 0644); err != nil {
			t.Fatalf("Mkfifo failed: %v", err)
		}

		pis, err := ListContents(dir)
		if err != nil {
			t.Fatalf("ListContents failed: %v", err)
		}

		// root dir + regular.txt only; FIFO should be ignored
		if len(pis) != 2 {
			t.Fatalf("Expected 2 entries (fifo should be ignored), got %d", len(pis))
		}
		for _, pi := range pis {
			if pi.Path == fifoPath {
				t.Error("FIFO should have been ignored but was included")
			}
		}
	})

	t.Run("Permissions", func(t *testing.T) {
		dir := t.TempDir()

		filePath := filepath.Join(dir, "perms.txt")
		if err := os.WriteFile(filePath, []byte("x"), 0755); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		pis, err := ListContents(dir)
		if err != nil {
			t.Fatalf("ListContents failed: %v", err)
		}

		var filePi *PathInfo
		for _, pi := range pis {
			if pi.Path == filePath {
				filePi = pi
				break
			}
		}
		if filePi == nil {
			t.Fatal("File entry not found")
		}
		if filePi.Mode.Perms != 0755 {
			t.Errorf("Expected perms 0755, got %04o", filePi.Mode.Perms)
		}
	})

	t.Run("UidGid", func(t *testing.T) {
		dir := t.TempDir()

		filePath := filepath.Join(dir, "owner.txt")
		if err := os.WriteFile(filePath, []byte("x"), 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		pis, err := ListContents(dir)
		if err != nil {
			t.Fatalf("ListContents failed: %v", err)
		}

		var filePi *PathInfo
		for _, pi := range pis {
			if pi.Path == filePath {
				filePi = pi
				break
			}
		}
		if filePi == nil {
			t.Fatal("File entry not found")
		}
		// The file should be owned by the current user
		if filePi.Uid != uint64(os.Getuid()) {
			t.Errorf("Expected UID %d, got %d", os.Getuid(), filePi.Uid)
		}
		if filePi.Gid != uint64(os.Getgid()) {
			t.Errorf("Expected GID %d, got %d", os.Getgid(), filePi.Gid)
		}
	})

	t.Run("SymlinkToDirectoryNotFollowed", func(t *testing.T) {
		dir := t.TempDir()

		// Create a subdirectory with a file in it
		subdir := filepath.Join(dir, "realdir")
		if err := os.Mkdir(subdir, 0755); err != nil {
			t.Fatalf("Mkdir failed: %v", err)
		}
		if err := os.WriteFile(filepath.Join(subdir, "inner.txt"), []byte("x"), 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		// Create a symlink pointing to the subdirectory
		dirLink := filepath.Join(dir, "linkdir")
		if err := os.Symlink("realdir", dirLink); err != nil {
			t.Fatalf("Symlink failed: %v", err)
		}

		pis, err := ListContents(dir)
		if err != nil {
			t.Fatalf("ListContents failed: %v", err)
		}

		// root dir + realdir + inner.txt + linkdir (symlink, not followed)
		if len(pis) != 4 {
			for _, pi := range pis {
				t.Logf("  %s %s (link=%q)", pi.Mode.Type, pi.Path, pi.Link)
			}
			t.Fatalf("Expected 4 entries, got %d", len(pis))
		}

		var linkPi *PathInfo
		for _, pi := range pis {
			if pi.Path == dirLink {
				linkPi = pi
				break
			}
		}
		if linkPi == nil {
			t.Fatal("Directory symlink entry not found")
		}
		if linkPi.Mode.Type != "l" {
			t.Errorf("Expected symlink type 'l', got %q", linkPi.Mode.Type)
		}
		if linkPi.Link != "realdir" {
			t.Errorf("Expected link target 'realdir', got %q", linkPi.Link)
		}
	})
}
