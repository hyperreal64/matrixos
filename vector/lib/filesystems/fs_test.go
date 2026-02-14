package filesystems

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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
	case "findmnt":
		if val := os.Getenv("MOCK_FINDMNT_OUTPUT"); val != "" {
			fmt.Fprint(os.Stdout, val)
		}
		if os.Getenv("MOCK_FINDMNT_EXIT_CODE") == "1" {
			os.Exit(1)
		}
	case "umount":
		if os.Getenv("MOCK_UMOUNT_FAIL") == "1" {
			fmt.Fprintln(os.Stderr, "umount failed")
			os.Exit(1)
		}
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
	case "mount":
		if os.Getenv("MOCK_MOUNT_FAIL") == "1" {
			fmt.Fprintln(os.Stderr, "mount failed")
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
	case "blkid":
		// Mock blkid output based on environment variables
		if val := os.Getenv("MOCK_BLKID_OUTPUT"); val != "" {
			fmt.Fprint(os.Stdout, val)
		}
		if os.Getenv("MOCK_BLKID_EXIT_CODE") == "1" {
			os.Exit(1)
		}
	case "udevadm", "blockdev":
		// No-op success
	default:
		// Pass for other commands
	}
	os.Exit(0)
}

func TestDeviceUUID(t *testing.T) {
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()

	t.Run("Success", func(t *testing.T) {
		expectedUUID := "1234-5678"
		os.Setenv("MOCK_BLKID_OUTPUT", expectedUUID)
		defer os.Unsetenv("MOCK_BLKID_OUTPUT")

		uuid, err := DeviceUUID("/dev/sda1")
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

	t.Run("CommandFail", func(t *testing.T) {
		os.Setenv("MOCK_BLKID_EXIT_CODE", "1")
		defer os.Unsetenv("MOCK_BLKID_EXIT_CODE")

		_, err := DeviceUUID("/dev/sda1")
		if err == nil {
			t.Error("Expected error from blkid failure, got nil")
		}
	})

	t.Run("NoOutput", func(t *testing.T) {
		os.Setenv("MOCK_BLKID_OUTPUT", "")
		defer os.Unsetenv("MOCK_BLKID_OUTPUT")

		_, err := DeviceUUID("/dev/sda1")
		if err == nil {
			t.Error("Expected error for no output, got nil")
		}
	})
}

func TestDevicePartUUID(t *testing.T) {
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()

	t.Run("Success", func(t *testing.T) {
		expectedPartUUID := "abcdef-01"
		os.Setenv("MOCK_BLKID_OUTPUT", expectedPartUUID)
		defer os.Unsetenv("MOCK_BLKID_OUTPUT")

		partuuid, err := DevicePartUUID("/dev/sda1")
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

	t.Run("CommandFail", func(t *testing.T) {
		os.Setenv("MOCK_BLKID_EXIT_CODE", "1")
		defer os.Unsetenv("MOCK_BLKID_EXIT_CODE")

		_, err := DevicePartUUID("/dev/sda1")
		if err == nil {
			t.Error("Expected error from blkid failure, got nil")
		}
	})

	t.Run("NoOutput", func(t *testing.T) {
		os.Setenv("MOCK_BLKID_OUTPUT", "")
		defer os.Unsetenv("MOCK_BLKID_OUTPUT")

		_, err := DevicePartUUID("/dev/sda1")
		if err == nil {
			t.Error("Expected error for no output, got nil")
		}
	})
}

func TestMountpointToDevice(t *testing.T) {
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()

	t.Run("Success", func(t *testing.T) {
		expectedDevice := "/dev/sda1"
		os.Setenv("MOCK_FINDMNT_OUTPUT", expectedDevice+"\n")
		defer os.Unsetenv("MOCK_FINDMNT_OUTPUT")

		device, err := MountpointToDevice("/mnt")
		if err != nil {
			t.Fatalf("MountpointToDevice failed: %v", err)
		}
		if device != expectedDevice {
			t.Errorf("Expected device %s, got %s", expectedDevice, device)
		}
	})

	t.Run("NoMountpoint", func(t *testing.T) {
		_, err := MountpointToDevice("")
		if err == nil {
			t.Error("Expected error for missing mountpoint, got nil")
		}
	})

	t.Run("CommandFail", func(t *testing.T) {
		os.Setenv("MOCK_FINDMNT_EXIT_CODE", "1")
		defer os.Unsetenv("MOCK_FINDMNT_EXIT_CODE")

		_, err := MountpointToDevice("/mnt")
		if err == nil {
			t.Error("Expected error from findmnt failure, got nil")
		}
	})

	t.Run("NoDeviceFound", func(t *testing.T) {
		os.Setenv("MOCK_FINDMNT_OUTPUT", "")
		defer os.Unsetenv("MOCK_FINDMNT_OUTPUT")

		_, err := MountpointToDevice("/mnt")
		if err == nil {
			t.Error("Expected error for no device found, got nil")
		}
	})
}

func TestListSubmounts(t *testing.T) {
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()

	t.Run("Success", func(t *testing.T) {
		mounts := "/mnt/test\n/mnt/test/sub"
		os.Setenv("MOCK_FINDMNT_OUTPUT", mounts)
		defer os.Unsetenv("MOCK_FINDMNT_OUTPUT")

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

	t.Run("CommandFail", func(t *testing.T) {
		os.Setenv("MOCK_FINDMNT_EXIT_CODE", "1")
		defer os.Unsetenv("MOCK_FINDMNT_EXIT_CODE")

		_, err := ListSubmounts("/mnt")
		if err == nil {
			t.Error("Expected error from findmnt failure, got nil")
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
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()

	t.Run("SuccessfulUnmount", func(t *testing.T) {
		os.Setenv("MOCK_FINDMNT_OUTPUT", "/mnt/test")
		defer os.Unsetenv("MOCK_FINDMNT_OUTPUT")
		CleanupMounts([]string{"/mnt/test"})
	})

	t.Run("MountNotExist", func(t *testing.T) {
		os.Unsetenv("MOCK_FINDMNT_OUTPUT")
		CleanupMounts([]string{"/mnt/test"})
	})

	t.Run("UnmountFail", func(t *testing.T) {
		os.Setenv("MOCK_FINDMNT_OUTPUT", "/mnt/fail")
		defer os.Unsetenv("MOCK_FINDMNT_OUTPUT")
		os.Setenv("MOCK_UMOUNT_FAIL", "1")
		defer os.Unsetenv("MOCK_UMOUNT_FAIL")
		// Should not panic or error out, just log
		CleanupMounts([]string{"/mnt/fail"})
	})
}

func TestSetupCommonRootfsMounts(t *testing.T) {
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()

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
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()

	src := t.TempDir()
	dst := t.TempDir()

	if _, err := BindMount(src, dst); err != nil {
		t.Errorf("BindMount failed: %v", err)
	}
}

func TestCleanupLoopDevices(t *testing.T) {
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()

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
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()

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
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()

	t.Run("NoActiveMounts", func(t *testing.T) {
		os.Unsetenv("MOCK_FINDMNT_OUTPUT")
		if err := CheckActiveMounts("/mnt/test"); err != nil {
			t.Errorf("CheckActiveMounts failed: %v", err)
		}
	})

	t.Run("ActiveMountsDetected", func(t *testing.T) {
		os.Setenv("MOCK_FINDMNT_OUTPUT", "/mnt/test/proc")
		defer os.Unsetenv("MOCK_FINDMNT_OUTPUT")
		if err := CheckActiveMounts("/mnt/test"); err == nil {
			t.Error("CheckActiveMounts should fail when mounts are detected")
		}
	})
}

func TestDevicesSettle(t *testing.T) {
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()
	// Simple execution test to ensure it runs without error
	DevicesSettle()
}

func TestFlushBlockDeviceBuffers(t *testing.T) {
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()

	t.Run("Success", func(t *testing.T) {
		if err := FlushBlockDeviceBuffers("/dev/sda"); err != nil {
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
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()

	t.Run("Success", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Mock that the mounts exist
		os.Setenv("MOCK_FINDMNT_OUTPUT", tmpDir)
		defer os.Unsetenv("MOCK_FINDMNT_OUTPUT")
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
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()

	t.Run("Success", func(t *testing.T) {
		tmpDir := t.TempDir()
		os.Setenv("MOCK_FINDMNT_OUTPUT", tmpDir)
		defer os.Unsetenv("MOCK_FINDMNT_OUTPUT")
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
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()

	t.Run("Success", func(t *testing.T) {
		distfilesDir := t.TempDir()
		rootfs := t.TempDir()
		if _, err := BindMountDistdir(distfilesDir, rootfs); err != nil {
			t.Errorf("BindMountDistdir failed: %v", err)
		}
	})
}

func TestBindUmountDistdir(t *testing.T) {
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()

	t.Run("Success", func(t *testing.T) {
		rootfs := t.TempDir()
		distfilesDir := filepath.Join(rootfs, "var", "cache", "distfiles")
		if err := os.MkdirAll(distfilesDir, 0755); err != nil {
			t.Fatal(err)
		}
		os.Setenv("MOCK_FINDMNT_OUTPUT", distfilesDir)
		defer os.Unsetenv("MOCK_FINDMNT_OUTPUT")

		if err := BindUmountDistdir(rootfs); err != nil {
			t.Errorf("BindUmountDistdir failed: %v", err)
		}
	})
}

func TestBindMountBinpkgs(t *testing.T) {
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()

	t.Run("Success", func(t *testing.T) {
		binpkgsDir := t.TempDir()
		rootfs := t.TempDir()
		if _, err := BindMountBinpkgs(binpkgsDir, rootfs); err != nil {
			t.Errorf("BindMountBinpkgs failed: %v", err)
		}
	})
}

func TestBindUmountBinpkgs(t *testing.T) {
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()

	t.Run("Success", func(t *testing.T) {
		rootfs := t.TempDir()
		binpkgsDir := filepath.Join(rootfs, "var", "cache", "binpkgs")
		if err := os.MkdirAll(binpkgsDir, 0755); err != nil {
			t.Fatal(err)
		}
		os.Setenv("MOCK_FINDMNT_OUTPUT", binpkgsDir)
		defer os.Unsetenv("MOCK_FINDMNT_OUTPUT")

		if err := BindUmountBinpkgs(rootfs); err != nil {
			t.Errorf("BindUmountBinpkgs failed: %v", err)
		}
	})
}

func TestCleanupCryptsetupDevices(t *testing.T) {
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()

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
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()

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
