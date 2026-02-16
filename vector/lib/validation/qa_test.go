package validation

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"matrixos/vector/lib/filesystems"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type MockConfig struct {
	items map[string]string
}

func (m *MockConfig) Load() error { return nil }
func (m *MockConfig) GetItem(key string) (string, error) {
	if v, ok := m.items[key]; ok {
		return v, nil
	}
	return "", fmt.Errorf("key not found: %s", key)
}
func (m *MockConfig) GetBool(key string) (bool, error)      { return false, nil }
func (m *MockConfig) GetItems(key string) ([]string, error) { return nil, nil }

// Mock helpers for command execution
var mockCmdOutput map[string]string

func fakeExecCommand(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	// pass env vars to identify the command
	// MUST append to os.Environ() so MOCK_SIG_KEY is passed through!
	env := os.Environ()
	env = append(env, "GO_WANT_HELPER_PROCESS=1",
		"GO_HELPER_CMD="+command,
		"GO_HELPER_ARGS="+strings.Join(args, " "))
	cmd.Env = env
	return cmd
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)

	cmd := os.Getenv("GO_HELPER_CMD")
	args := os.Getenv("GO_HELPER_ARGS")

	// Special mocking logic based on arguments
	// Specifically for modinfo
	if cmd == "unshare" && strings.Contains(args, "modinfo -F sig_key") {
		// return a dummy signature key
		// The key must match what we expect in the test
		// If test sets special env var, use it
		fmt.Print(os.Getenv("MOCK_SIG_KEY"))
		return
	}

	if cmd == "unshare" && strings.Contains(args, "modinfo -F vermagic") {
		// Mock vermagic
		fmt.Print("5.15.0 SMP mod_unload")
		return
	}

	if cmd == "file" && strings.Contains(args, "-b") {
		// Mock file output for vmlinuz
		fmt.Print("Linux kernel x86 boot executable bzImage, version 5.15.0 (root@host) #1 SMP ...")
		return
	}

	// default
	fmt.Fprintf(os.Stderr, "Unknown mock command: %s %s\n", cmd, args)
	os.Exit(1)
}

func TestRootPrivs(t *testing.T) {
	// Behavior depends on test runner (may be root in some CI); assert consistent result
	q, _ := New(&MockConfig{})
	err := q.RootPrivs()
	if os.Geteuid() == 0 {
		if err != nil {
			t.Fatalf("expected nil when running as root, got %v", err)
		}
	} else {
		if err == nil {
			t.Fatalf("expected error when not running as root, got nil")
		}
	}
}

func TestCheckMatrixOSPrivate(t *testing.T) {
	tmp := t.TempDir()
	cfg := &MockConfig{items: map[string]string{
		"matrixOS.PrivateGitRepoPath": tmp,
	}}
	q, _ := New(cfg)

	if err := q.CheckMatrixOSPrivate(); err != nil {
		t.Fatalf("expected nil for existing dir, got %v", err)
	}

	// non-existent
	cfg.items["matrixOS.PrivateGitRepoPath"] = filepath.Join(tmp, "nope")
	if err := q.CheckMatrixOSPrivate(); err == nil {
		t.Fatalf("expected error for missing dir, got nil")
	}
}

func TestCheckNumberOfKernels(t *testing.T) {
	tmp := t.TempDir()
	// create usr/lib/modules/v1/vmlinuz
	p := filepath.Join(tmp, "usr/lib/modules/v1")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	v := filepath.Join(p, "vmlinuz")
	if err := os.WriteFile(v, []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}

	q, _ := New(&MockConfig{})
	if err := q.CheckNumberOfKernels(tmp, 1); err != nil {
		t.Fatalf("expected nil when count matches, got %v", err)
	}
	if err := q.CheckNumberOfKernels(tmp, 2); err == nil {
		t.Fatalf("expected error when count mismatches, got nil")
	}
}

func TestVerifyEnvironmentSetup(t *testing.T) {
	tmp := t.TempDir()
	// create a directory that will be used as dir arg
	d := "/usr/share/shim"
	if err := os.MkdirAll(filepath.Join(tmp, d), 0o755); err != nil {
		t.Fatal(err)
	}
	// create a /bin/sh inside the temp image
	binsh := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binsh, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binsh, "sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// choose an executable that exists (sh) and a path-like executable inside image
	exes := []string{"sh", "/bin/sh"}
	dirs := []string{d}
	// Since verifyEnvironmentSetup is now private or just used internally by methods that are on QA,
	// but verifyEnvironmentSetup itself is a plain function in my last edit?
	// Let's check `qa.go` content. I kept it as a private function `verifyEnvironmentSetup`.
	// So I can test it directly if I am in the same package.
	if err := verifyEnvironmentSetup(tmp, exes, dirs); err != nil {
		t.Fatalf("expected nil environment setup, got %v", err)
	}
}

func TestCheckSecureBoot(t *testing.T) {
	// Setup overrides
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()
	filesystems.ExecCommand = fakeExecCommand
	defer func() { filesystems.ExecCommand = exec.Command }()

	tmp := t.TempDir()
	// Create dummy usb-storage.ko
	modDir := filepath.Join(tmp, "lib/modules/5.15.0")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modDir, "usb-storage.ko"), []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create dummy cert
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	serialNumber := big.NewInt(123456)
	template := x509.Certificate{
		SerialNumber: serialNumber,
	}
	certDer, _ := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	certPem := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDer})
	certPath := filepath.Join(tmp, "cert.pem")
	if err := os.WriteFile(certPath, certPem, 0o644); err != nil {
		t.Fatal(err)
	}

	// We expect the mock command to return the serial: 01:e2:40 (123456 in hex)
	// OpenSSL serial format is usually lower case hex colon separated.
	// 123456 = 0x1E240 => 01:e2:40 (even lengthed).
	os.Setenv("MOCK_SIG_KEY", "01:e2:40")
	defer os.Unsetenv("MOCK_SIG_KEY")

	q, err := New(&MockConfig{})
	if err != nil {
		t.Fatal(err)
	}

	if err := q.CheckSecureBoot(tmp, certPath); err != nil {
		t.Fatalf("expected nil for matching serial, got %v", err)
	}

	// Mismatch
	os.Setenv("MOCK_SIG_KEY", "aa:bb:cc")
	if err := q.CheckSecureBoot(tmp, certPath); err == nil {
		t.Fatalf("expected error for mismatched serial, got nil")
	}
}

func TestCheckKernelAndExternalModule(t *testing.T) {
	// Setup overrides
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()
	filesystems.ExecCommand = fakeExecCommand
	defer func() { filesystems.ExecCommand = exec.Command }()

	tmp := t.TempDir()
	modVer := "5.15.0"
	modDir := filepath.Join(tmp, "lib/modules", modVer)
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create vmlinuz and initramfs
	if err := os.WriteFile(filepath.Join(modDir, "vmlinuz"), []byte("kernel"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modDir, "initramfs"), []byte("initramfs"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create dummy module
	if err := os.WriteFile(filepath.Join(modDir, "nvidia.ko"), []byte("mod"), 0o644); err != nil {
		t.Fatal(err)
	}

	q, _ := New(&MockConfig{})
	// Mock returns 5.15.0 which matches our dir structure
	if err := q.CheckKernelAndExternalModule(tmp, "nvidia.ko*"); err != nil {
		t.Fatalf("expected nil for valid module check, got %v", err)
	}
}

func TestVerifyWrappers(t *testing.T) {
	tmp := t.TempDir()
	cfg := &MockConfig{items: map[string]string{
		"matrixOS.PrivateGitRepoPath": tmp,
	}}
	q, _ := New(cfg)

	// We can't easily test success without creating TONS of binaries files inside tmp
	// But we can test failure if dir is empty (which it is)

	// Create minimal structure to avoid "imagedir missing" error
	if err := q.VerifyDistroRootfsEnvironmentSetup(tmp); err == nil {
		t.Fatalf("expected error for empty image dir (missing binaries), got nil")
	}
	// We expect VerifySeederEnvironmentSetup to look for chroot etc.
	if err := q.VerifySeederEnvironmentSetup(tmp); err == nil {
		t.Fatalf("expected error for empty image dir, got nil")
	}
}
