package filesystems

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mockConfig implements config.IConfig for fsenc testing.
type mockConfig struct {
	Items map[string][]string
	Bools map[string]bool
}

func (m *mockConfig) Load() error {
	return nil
}

func (m *mockConfig) GetItem(key string) (string, error) {
	if lst, ok := m.Items[key]; ok {
		var val string
		if len(lst) > 0 {
			val = lst[len(lst)-1]
		}
		return val, nil
	}
	return "", nil
}

func (m *mockConfig) GetItems(key string) ([]string, error) {
	if val, ok := m.Items[key]; ok {
		return val, nil
	}
	return nil, nil
}

func (m *mockConfig) GetBool(key string) (bool, error) {
	if val, ok := m.Bools[key]; ok {
		return val, nil
	}
	return false, nil
}

// errConfig returns errors for every call, useful for testing error paths.
type errConfig struct {
	err error
}

func (e *errConfig) Load() error                       { return e.err }
func (e *errConfig) GetItem(string) (string, error)    { return "", e.err }
func (e *errConfig) GetItems(string) ([]string, error) { return nil, e.err }
func (e *errConfig) GetBool(string) (bool, error)      { return false, e.err }

// mockRunner records calls and returns a configurable error.
type mockRunner struct {
	calls []mockRunnerCall
	err   error
	// failOn allows failing on a specific call index (0-based).
	failOn int
}

type mockRunnerCall struct {
	Name string
	Args []string
}

func (mr *mockRunner) run(stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
	mr.calls = append(mr.calls, mockRunnerCall{Name: name, Args: args})
	if mr.failOn >= 0 && len(mr.calls)-1 == mr.failOn {
		return mr.err
	}
	if mr.failOn < 0 && mr.err != nil {
		return mr.err
	}
	return nil
}

func newMockRunner() *mockRunner {
	return &mockRunner{failOn: -1}
}

func newMockRunnerFailOnCall(n int, err error) *mockRunner {
	return &mockRunner{failOn: n, err: err}
}

func baseFsencConfig() *mockConfig {
	return &mockConfig{
		Items: map[string][]string{
			"Imager.EncryptionKey":       {"superSecret123"},
			"Imager.EncryptedRootFsName": {"matrixosroot"},
			"matrixOS.OsName":            {"matrixos"},
		},
		Bools: map[string]bool{
			"Imager.Encryption": true,
		},
	}
}

// --- NewFsenc Tests ---

func TestNewFsenc(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		cfg := baseFsencConfig()
		f, err := NewFsenc(cfg)
		if err != nil {
			t.Fatalf("NewFsenc() returned error: %v", err)
		}
		if f == nil {
			t.Fatal("NewFsenc() returned nil")
		}
	})

	t.Run("NilConfig", func(t *testing.T) {
		_, err := NewFsenc(nil)
		if err == nil {
			t.Fatal("NewFsenc(nil) should return error")
		}
	})
}

// --- Config Accessor Tests ---

func TestEncryptionEnabled(t *testing.T) {
	t.Run("Enabled", func(t *testing.T) {
		cfg := baseFsencConfig()
		f, _ := NewFsenc(cfg)
		enabled, err := f.EncryptionEnabled()
		if err != nil {
			t.Fatalf("EncryptionEnabled() error: %v", err)
		}
		if !enabled {
			t.Error("Expected encryption to be enabled")
		}
	})

	t.Run("Disabled", func(t *testing.T) {
		cfg := baseFsencConfig()
		cfg.Bools["Imager.Encryption"] = false
		f, _ := NewFsenc(cfg)
		enabled, err := f.EncryptionEnabled()
		if err != nil {
			t.Fatalf("EncryptionEnabled() error: %v", err)
		}
		if enabled {
			t.Error("Expected encryption to be disabled")
		}
	})

	t.Run("ConfigError", func(t *testing.T) {
		ec := &errConfig{err: errors.New("cfg error")}
		f, _ := NewFsenc(ec)
		_, err := f.EncryptionEnabled()
		if err == nil {
			t.Error("Expected error from broken config")
		}
	})
}

func TestEncryptionKey(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		cfg := baseFsencConfig()
		f, _ := NewFsenc(cfg)
		key, err := f.EncryptionKey()
		if err != nil {
			t.Fatalf("EncryptionKey() error: %v", err)
		}
		if key != "superSecret123" {
			t.Errorf("Expected 'superSecret123', got %q", key)
		}
	})

	t.Run("ConfigError", func(t *testing.T) {
		ec := &errConfig{err: errors.New("cfg error")}
		f, _ := NewFsenc(ec)
		_, err := f.EncryptionKey()
		if err == nil {
			t.Error("Expected error from broken config")
		}
	})
}

func TestEncryptedRootFsName(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		cfg := baseFsencConfig()
		f, _ := NewFsenc(cfg)
		name, err := f.EncryptedRootFsName()
		if err != nil {
			t.Fatalf("EncryptedRootFsName() error: %v", err)
		}
		if name != "matrixosroot" {
			t.Errorf("Expected 'matrixosroot', got %q", name)
		}
	})

	t.Run("Empty", func(t *testing.T) {
		cfg := baseFsencConfig()
		cfg.Items["Imager.EncryptedRootFsName"] = []string{""}
		f, _ := NewFsenc(cfg)
		_, err := f.EncryptedRootFsName()
		if err == nil {
			t.Error("Expected error for empty EncryptedRootFsName")
		}
	})

	t.Run("ConfigError", func(t *testing.T) {
		ec := &errConfig{err: errors.New("cfg error")}
		f, _ := NewFsenc(ec)
		_, err := f.EncryptedRootFsName()
		if err == nil {
			t.Error("Expected error from broken config")
		}
	})
}

func TestFsencOsName(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		cfg := baseFsencConfig()
		f, _ := NewFsenc(cfg)
		name, err := f.OsName()
		if err != nil {
			t.Fatalf("OsName() error: %v", err)
		}
		if name != "matrixos" {
			t.Errorf("Expected 'matrixos', got %q", name)
		}
	})

	t.Run("Empty", func(t *testing.T) {
		cfg := baseFsencConfig()
		cfg.Items["matrixOS.OsName"] = []string{""}
		f, _ := NewFsenc(cfg)
		_, err := f.OsName()
		if err == nil {
			t.Error("Expected error for empty OsName")
		}
	})

	t.Run("ConfigError", func(t *testing.T) {
		ec := &errConfig{err: errors.New("cfg error")}
		f, _ := NewFsenc(ec)
		_, err := f.OsName()
		if err == nil {
			t.Error("Expected error from broken config")
		}
	})
}

// --- MountImageAsLoopDevice Tests ---

func TestMountImageAsLoopDevice(t *testing.T) {
	origExec := ExecCommand
	defer func() { ExecCommand = origExec }()
	ExecCommand = fakeExecCommand

	t.Run("Success", func(t *testing.T) {
		t.Setenv("MOCK_LOSETUP_OUTPUT", "/dev/loop7\n")
		cfg := baseFsencConfig()
		f, _ := NewFsenc(cfg)
		dev, err := f.MountImageAsLoopDevice("/tmp/disk.img")
		if err != nil {
			t.Fatalf("MountImageAsLoopDevice() error: %v", err)
		}
		if dev != "/dev/loop7" {
			t.Errorf("Expected /dev/loop7, got %q", dev)
		}
	})

	t.Run("EmptyImagePath", func(t *testing.T) {
		cfg := baseFsencConfig()
		f, _ := NewFsenc(cfg)
		_, err := f.MountImageAsLoopDevice("")
		if err == nil {
			t.Error("Expected error for empty imagePath")
		}
	})

	t.Run("CommandFail", func(t *testing.T) {
		t.Setenv("MOCK_LOSETUP_FAIL", "1")
		cfg := baseFsencConfig()
		f, _ := NewFsenc(cfg)
		_, err := f.MountImageAsLoopDevice("/tmp/disk.img")
		if err == nil {
			t.Error("Expected error from losetup failure")
		}
	})

	t.Run("EmptyOutput", func(t *testing.T) {
		t.Setenv("MOCK_LOSETUP_OUTPUT", "")
		cfg := baseFsencConfig()
		f, _ := NewFsenc(cfg)
		_, err := f.MountImageAsLoopDevice("/tmp/disk.img")
		if err == nil {
			t.Error("Expected error for empty losetup output")
		}
	})
}

// --- LuksEncrypt Tests ---

func TestLuksEncrypt(t *testing.T) {
	t.Run("EmptyDevicePath", func(t *testing.T) {
		cfg := baseFsencConfig()
		f, _ := NewFsenc(cfg)
		var dm []string
		err := f.LuksEncrypt("", "/dev/mapper/root", &dm)
		if err == nil {
			t.Error("Expected error for empty devicePath")
		}
	})

	t.Run("EmptyDesiredLuksDevice", func(t *testing.T) {
		cfg := baseFsencConfig()
		f, _ := NewFsenc(cfg)
		var dm []string
		err := f.LuksEncrypt("/dev/loop0p3", "", &dm)
		if err == nil {
			t.Error("Expected error for empty desiredLuksDevice")
		}
	})

	t.Run("NilDeviceMappers", func(t *testing.T) {
		cfg := baseFsencConfig()
		f, _ := NewFsenc(cfg)
		err := f.LuksEncrypt("/dev/loop0p3", "/dev/mapper/root", nil)
		if err == nil {
			t.Error("Expected error for nil deviceMappers")
		}
	})

	t.Run("EncryptionKeyError", func(t *testing.T) {
		ec := &errConfig{err: errors.New("key error")}
		f, _ := NewFsenc(ec)
		var dm []string
		err := f.LuksEncrypt("/dev/loop0p3", "/dev/mapper/root", &dm)
		if err == nil {
			t.Error("Expected error from config failure")
		}
	})

	t.Run("LuksFormatFails", func(t *testing.T) {
		cfg := baseFsencConfig()
		f, _ := NewFsenc(cfg)
		mr := newMockRunnerFailOnCall(0, errors.New("luksFormat failed"))
		f.runner = mr.run

		var dm []string
		err := f.LuksEncrypt("/dev/loop0p3", "/dev/mapper/matrixosroot", &dm)
		if err == nil {
			t.Fatal("Expected error from luksFormat failure")
		}
		if !strings.Contains(err.Error(), "luksFormat") {
			t.Errorf("Error should mention luksFormat: %v", err)
		}
	})

	t.Run("CryptsetupOpenFails", func(t *testing.T) {
		cfg := baseFsencConfig()
		f, _ := NewFsenc(cfg)
		mr := newMockRunnerFailOnCall(1, errors.New("open failed"))
		f.runner = mr.run

		var dm []string
		err := f.LuksEncrypt("/dev/loop0p3", "/dev/mapper/matrixosroot", &dm)
		if err == nil {
			t.Fatal("Expected error from cryptsetup open failure")
		}
		if !strings.Contains(err.Error(), "cryptsetup open") {
			t.Errorf("Error should mention cryptsetup open: %v", err)
		}
	})

	t.Run("DesiredLuksDeviceDoesNotExist", func(t *testing.T) {
		cfg := baseFsencConfig()
		f, _ := NewFsenc(cfg)
		mr := newMockRunner()
		f.runner = mr.run

		var dm []string
		// Use a path that won't exist.
		err := f.LuksEncrypt("/dev/loop0p3", "/dev/mapper/nonexistent_test_device", &dm)
		if err == nil {
			t.Fatal("Expected error because desired LUKS device does not exist")
		}
		if !strings.Contains(err.Error(), "does not exist") {
			t.Errorf("Error should mention 'does not exist': %v", err)
		}
		// The device mapper name should still be tracked for cleanup.
		if len(dm) != 1 || dm[0] != "nonexistent_test_device" {
			t.Errorf("Expected deviceMappers to contain 'nonexistent_test_device', got %v", dm)
		}
	})

	t.Run("SuccessWithPassphrase", func(t *testing.T) {
		// Create a fake LUKS device path that "exists" after open.
		tmpDir := t.TempDir()
		desiredDevice := filepath.Join(tmpDir, "matrixosroot")
		os.WriteFile(desiredDevice, []byte{}, 0600)

		cfg := baseFsencConfig()
		f, _ := NewFsenc(cfg)
		mr := newMockRunner()
		f.runner = mr.run

		var dm []string
		err := f.LuksEncrypt("/dev/loop0p3", desiredDevice, &dm)
		if err != nil {
			t.Fatalf("LuksEncrypt() error: %v", err)
		}

		// Should have made 2 runner calls: luksFormat + open.
		if len(mr.calls) != 2 {
			t.Fatalf("Expected 2 runner calls, got %d", len(mr.calls))
		}

		// Verify luksFormat call.
		if mr.calls[0].Name != "cryptsetup" {
			t.Errorf("Call 0: expected 'cryptsetup', got %q", mr.calls[0].Name)
		}
		if !containsArg(mr.calls[0].Args, "luksFormat") {
			t.Errorf("Call 0 should contain 'luksFormat': %v", mr.calls[0].Args)
		}
		// Key is a passphrase (not a file), so key-file should be "-".
		if !containsArg(mr.calls[0].Args, "-") {
			t.Errorf("Call 0 should use '-' as key file for passphrase mode: %v", mr.calls[0].Args)
		}

		// Verify open call.
		if !containsArg(mr.calls[1].Args, "open") {
			t.Errorf("Call 1 should contain 'open': %v", mr.calls[1].Args)
		}
		if !containsArg(mr.calls[1].Args, "--allow-discards") {
			t.Errorf("Call 1 should contain '--allow-discards': %v", mr.calls[1].Args)
		}

		// Verify device mapper tracking.
		expectedName := filepath.Base(desiredDevice)
		if len(dm) != 1 || dm[0] != expectedName {
			t.Errorf("Expected deviceMappers to contain %q, got %v", expectedName, dm)
		}
	})

	t.Run("SuccessWithKeyFile", func(t *testing.T) {
		// Create a real key file and a fake LUKS device.
		tmpDir := t.TempDir()
		keyFile := filepath.Join(tmpDir, "enc.key")
		os.WriteFile(keyFile, []byte("file-based-key"), 0600)
		desiredDevice := filepath.Join(tmpDir, "matrixosroot")
		os.WriteFile(desiredDevice, []byte{}, 0600)

		cfg := baseFsencConfig()
		cfg.Items["Imager.EncryptionKey"] = []string{keyFile}
		f, _ := NewFsenc(cfg)
		mr := newMockRunner()
		f.runner = mr.run

		var dm []string
		err := f.LuksEncrypt("/dev/loop0p3", desiredDevice, &dm)
		if err != nil {
			t.Fatalf("LuksEncrypt() error: %v", err)
		}

		// luksFormat should use the actual key file path, not "-".
		if !containsArg(mr.calls[0].Args, keyFile) {
			t.Errorf("Call 0 should use key file path %q: %v", keyFile, mr.calls[0].Args)
		}
		// open should reference the key file.
		expectedKeyFileArg := "--key-file=" + keyFile
		if !containsArg(mr.calls[1].Args, expectedKeyFileArg) {
			t.Errorf("Call 1 should use %q: %v", expectedKeyFileArg, mr.calls[1].Args)
		}
	})
}

// --- LuksBackupHeader Tests ---

func TestLuksBackupHeader(t *testing.T) {
	t.Run("EmptyDevicePath", func(t *testing.T) {
		cfg := baseFsencConfig()
		f, _ := NewFsenc(cfg)
		err := f.LuksBackupHeader("", "/mnt/efi")
		if err == nil {
			t.Error("Expected error for empty devicePath")
		}
	})

	t.Run("EmptyMountEfifs", func(t *testing.T) {
		cfg := baseFsencConfig()
		f, _ := NewFsenc(cfg)
		err := f.LuksBackupHeader("/dev/loop0p3", "")
		if err == nil {
			t.Error("Expected error for empty mountEfifs")
		}
	})

	t.Run("OsNameError", func(t *testing.T) {
		cfg := baseFsencConfig()
		cfg.Items["matrixOS.OsName"] = []string{""}
		f, _ := NewFsenc(cfg)
		err := f.LuksBackupHeader("/dev/loop0p3", "/mnt/efi")
		if err == nil {
			t.Error("Expected error from empty OsName")
		}
	})

	t.Run("RunnerFails", func(t *testing.T) {
		cfg := baseFsencConfig()
		f, _ := NewFsenc(cfg)
		mr := newMockRunnerFailOnCall(0, errors.New("backup failed"))
		f.runner = mr.run

		err := f.LuksBackupHeader("/dev/loop0p3", "/mnt/efi")
		if err == nil {
			t.Fatal("Expected error from cryptsetup failure")
		}
		if !strings.Contains(err.Error(), "luksHeaderBackup") {
			t.Errorf("Error should mention luksHeaderBackup: %v", err)
		}
	})

	t.Run("Success", func(t *testing.T) {
		cfg := baseFsencConfig()
		f, _ := NewFsenc(cfg)
		mr := newMockRunner()
		f.runner = mr.run

		err := f.LuksBackupHeader("/dev/loop0p3", "/mnt/efi")
		if err != nil {
			t.Fatalf("LuksBackupHeader() error: %v", err)
		}

		if len(mr.calls) != 1 {
			t.Fatalf("Expected 1 runner call, got %d", len(mr.calls))
		}
		if mr.calls[0].Name != "cryptsetup" {
			t.Errorf("Expected 'cryptsetup', got %q", mr.calls[0].Name)
		}
		if !containsArg(mr.calls[0].Args, "luksHeaderBackup") {
			t.Errorf("Should contain 'luksHeaderBackup': %v", mr.calls[0].Args)
		}

		expectedBackup := "/mnt/efi/matrixos-rootfs-luks-header-backup.img"
		if !containsArg(mr.calls[0].Args, expectedBackup) {
			t.Errorf("Should contain backup path %q: %v", expectedBackup, mr.calls[0].Args)
		}
	})
}

// --- ValidateLuksVariables Tests ---

func TestValidateLuksVariables(t *testing.T) {
	t.Run("EncryptionDisabled", func(t *testing.T) {
		cfg := baseFsencConfig()
		cfg.Bools["Imager.Encryption"] = false
		f, _ := NewFsenc(cfg)
		err := f.ValidateLuksVariables()
		if err != nil {
			t.Fatalf("ValidateLuksVariables() should succeed when encryption is disabled: %v", err)
		}
	})

	t.Run("EncryptionEnabledAllSet", func(t *testing.T) {
		cfg := baseFsencConfig()
		f, _ := NewFsenc(cfg)
		err := f.ValidateLuksVariables()
		if err != nil {
			t.Fatalf("ValidateLuksVariables() error: %v", err)
		}
	})

	t.Run("EncryptionEnabledMissingKey", func(t *testing.T) {
		cfg := baseFsencConfig()
		cfg.Items["Imager.EncryptionKey"] = []string{""}
		f, _ := NewFsenc(cfg)
		err := f.ValidateLuksVariables()
		if err == nil {
			t.Error("Expected error for missing EncryptionKey")
		}
		if !strings.Contains(err.Error(), "EncryptionKey") {
			t.Errorf("Error should mention EncryptionKey: %v", err)
		}
	})

	t.Run("EncryptionEnabledMissingRootFsName", func(t *testing.T) {
		cfg := baseFsencConfig()
		cfg.Items["Imager.EncryptedRootFsName"] = []string{""}
		f, _ := NewFsenc(cfg)
		err := f.ValidateLuksVariables()
		if err == nil {
			t.Error("Expected error for missing EncryptedRootFsName")
		}
		if !strings.Contains(err.Error(), "EncryptedRootFsName") {
			t.Errorf("Error should mention EncryptedRootFsName: %v", err)
		}
	})

	t.Run("EncryptionCheckConfigError", func(t *testing.T) {
		ec := &errConfig{err: errors.New("cfg broken")}
		f, _ := NewFsenc(ec)
		err := f.ValidateLuksVariables()
		if err == nil {
			t.Error("Expected error from broken config")
		}
	})
}

// --- FileExists / PathExists / DirectoryExists Tests ---

func TestFileExistsHelper(t *testing.T) {
	tmpDir := t.TempDir()
	file := filepath.Join(tmpDir, "testfile")
	os.WriteFile(file, []byte("data"), 0644)

	if !FileExists(file) {
		t.Error("FileExists(file) should be true")
	}
	if FileExists(tmpDir) {
		t.Error("FileExists(dir) should be false")
	}
	if FileExists(filepath.Join(tmpDir, "nonexistent")) {
		t.Error("FileExists(nonexistent) should be false")
	}
}

func TestPathExistsHelper(t *testing.T) {
	tmpDir := t.TempDir()
	file := filepath.Join(tmpDir, "testfile")
	os.WriteFile(file, []byte("data"), 0644)

	if !PathExists(file) {
		t.Error("PathExists(file) should be true")
	}
	if !PathExists(tmpDir) {
		t.Error("PathExists(dir) should be true")
	}
	if PathExists(filepath.Join(tmpDir, "nonexistent")) {
		t.Error("PathExists(nonexistent) should be false")
	}
}

func TestDirectoryExistsHelper(t *testing.T) {
	tmpDir := t.TempDir()
	file := filepath.Join(tmpDir, "testfile")
	os.WriteFile(file, []byte("data"), 0644)

	if !DirectoryExists(tmpDir) {
		t.Error("DirectoryExists(dir) should be true")
	}
	if DirectoryExists(file) {
		t.Error("DirectoryExists(file) should be false")
	}
	if DirectoryExists(filepath.Join(tmpDir, "nonexistent")) {
		t.Error("DirectoryExists(nonexistent) should be false")
	}
}

// --- IFsenc Interface Compliance ---

func TestFsencImplementsIFsenc(t *testing.T) {
	cfg := baseFsencConfig()
	f, _ := NewFsenc(cfg)
	// Compile-time interface check.
	var _ IFsenc = f
}

// --- Helpers ---

func containsArg(args []string, target string) bool {
	for _, a := range args {
		if a == target {
			return true
		}
	}
	return false
}

// --- DefaultCommandRunner Integration (uses fakeExecCommand) ---

func TestDefaultCommandRunner(t *testing.T) {
	origExec := ExecCommand
	defer func() { ExecCommand = origExec }()
	ExecCommand = fakeExecCommand

	t.Run("Success", func(t *testing.T) {
		err := defaultCommandRunner(nil, os.Stdout, os.Stderr, "udevadm", "settle")
		if err != nil {
			t.Fatalf("defaultCommandRunner() error: %v", err)
		}
	})

	t.Run("WithStdin", func(t *testing.T) {
		stdin := strings.NewReader("input-data")
		err := defaultCommandRunner(stdin, os.Stdout, os.Stderr, "udevadm", "settle")
		if err != nil {
			t.Fatalf("defaultCommandRunner() with stdin error: %v", err)
		}
	})

	t.Run("CommandFail", func(t *testing.T) {
		t.Setenv("MOCK_LOSETUP_FAIL", "1")
		err := defaultCommandRunner(nil, os.Stdout, os.Stderr, "losetup", "-d", "/dev/loop99")
		if err == nil {
			t.Fatal("Expected error from failing command")
		}
	})
}

// TestHelperProcess is required for fakeExecCommand from fs_test.go.
// Since fakeExecCommand is already defined in fs_test.go (same package),
// it re-uses the TestHelperProcess defined there.
// If this file is built standalone, we need the helper below.
// We check GO_WANT_HELPER_PROCESS to avoid running as an actual test.
func TestHelperProcessFsenc(t *testing.T) {
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

	cmd := args[0]
	switch cmd {
	case "losetup":
		if val := os.Getenv("MOCK_LOSETUP_OUTPUT"); val != "" {
			fmt.Fprint(os.Stdout, val)
		}
		if os.Getenv("MOCK_LOSETUP_FAIL") == "1" {
			os.Exit(1)
		}
	case "cryptsetup":
		if os.Getenv("MOCK_CRYPTSETUP_FAIL") == "1" {
			fmt.Fprintln(os.Stderr, "cryptsetup failed")
			os.Exit(1)
		}
	case "udevadm":
		// No-op success
	default:
		// Pass for other commands
	}
	os.Exit(0)
}
