package commands

import (
	"bytes"
	"fmt"
	"io"
	"matrixos/vector/lib/cds"
	fslib "matrixos/vector/lib/filesystems"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	mockCurrentSHA = "old-sha"
	mockNewSHA     = "new-sha"
	mockRefSpec    = "remote:branch"
	stateroot      = "matrixos"
)

// newTestUpgradeCommand creates an UpgradeCommand with injected mock dependencies,
// bypassing initConfig/initOstree which require real config files and ostree binary.
func newTestUpgradeCommand(ot cds.IOstree, args []string) (*UpgradeCommand, error) {
	cmd := &UpgradeCommand{}
	cmd.ot = ot
	cmd.StartUI()
	if err := cmd.parseArgs(args); err != nil {
		return nil, err
	}
	return cmd, nil
}

// newTestUpgradeCommandWithConfig creates an UpgradeCommand with mock cds.IOstree and
// a real config from a file, for tests that need config values (e.g. bootloader).
func newTestUpgradeCommandWithConfig(ot cds.IOstree, cfg *testConfig, args []string) (*UpgradeCommand, error) {
	cmd := &UpgradeCommand{}
	cmd.ot = ot
	cmd.cfg = cfg
	cmd.StartUI()
	if err := cmd.parseArgs(args); err != nil {
		return nil, err
	}
	return cmd, nil
}

// testConfig is a minimal IConfig for test use. It reads values from a map.
type testConfig struct {
	items map[string]string
}

func (c *testConfig) Load() error { return nil }
func (c *testConfig) GetItem(key string) (string, error) {
	v, ok := c.items[key]
	if !ok {
		return "", fmt.Errorf("invalid key: %s", key)
	}
	return v, nil
}
func (c *testConfig) GetBool(key string) (bool, error) { return false, nil }
func (c *testConfig) GetItems(key string) ([]string, error) {
	v, ok := c.items[key]
	if !ok {
		return nil, nil
	}
	return []string{v}, nil
}

// upgradeHarness holds common test state for upgrade tests.
type upgradeHarness struct {
	mock    *mockOstree
	cleanup func()
}

func setupUpgradeHarness(t *testing.T, currentSHA, newSHA string) *upgradeHarness {
	t.Helper()

	origEuid := getEuid
	getEuid = func() int { return 0 }

	// Mock execCommand for ostree ls (package listing)
	origExec := execCommand
	execCommand = func(command string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestUpgradeHelperProcess", "--", command}
		cs = append(cs, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = []string{
			"GO_WANT_UPGRADE_HELPER_PROCESS=1",
			"TEST_CURRENT_SHA=" + currentSHA,
			"TEST_NEW_SHA=" + newSHA,
		}
		return cmd
	}

	mock := &mockOstree{
		root: "/",
		deployments: []cds.Deployment{
			{
				Booted:    true,
				Checksum:  currentSHA,
				Stateroot: stateroot,
				Refspec:   mockRefSpec,
			},
		},
		lastCommit: newSHA,
		packagesByCommit: map[string][]string{
			currentSHA: {"app-misc/foo-1.0"},
			newSHA:     {"app-misc/foo-1.1"},
		},
	}

	return &upgradeHarness{
		mock: mock,
		cleanup: func() {
			getEuid = origEuid
			execCommand = origExec
		},
	}
}

func runCaptureStdout(f func() error) (string, error) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := f()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return stripAnsi(buf.String()), err
}

func stripAnsi(str string) string {
	const ansi = "[\u001B\u009B][[\\]()#;?]*(?:(?:(?:[a-zA-Z\\d]*(?:;[a-zA-Z\\d]*)*)?\u0007)|(?:(?:\\d{1,4}(?:;\\d{0,4})*)?[\\dA-PRZcf-ntqry=><~]))"
	re := regexp.MustCompile(ansi)
	return re.ReplaceAllString(str, "")
}

// TestUpgradeHelperProcess is a subprocess helper for execCommand mocking.
// It only handles "ostree ls" (for package listing) and "sbverify".
func TestUpgradeHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_UPGRADE_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)

	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}
	if len(args) == 0 {
		os.Exit(1)
	}

	cmd := args[0]

	// Handle sbverify (always succeeds)
	if cmd == "sbverify" {
		return
	}

	// Handle "ostree ls -R <commit> -- <path>"
	if cmd == "ostree" {
		for _, arg := range args {
			if strings.Contains(arg, "/usr/var-db-pkg") {
				os.Exit(1)
				return
			}
		}

		currentSHA := os.Getenv("TEST_CURRENT_SHA")
		newSHA := os.Getenv("TEST_NEW_SHA")

		mockPackages := map[string][]string{
			currentSHA: {
				"d00755 0 0 0 /var/db/pkg/app-misc/foo-1.0/",
				"-00644 0 0 0 /var/db/pkg/app-misc/foo-1.0/CONTENTS",
			},
			newSHA: {
				"d00755 0 0 0 /var/db/pkg/app-misc/foo-1.1/",
				"-00644 0 0 0 /var/db/pkg/app-misc/foo-1.1/CONTENTS",
			},
		}

		for _, arg := range args {
			if pkgs, ok := mockPackages[arg]; ok {
				for _, line := range pkgs {
					fmt.Println(line)
				}
				return
			}
		}
	}

	os.Exit(1)
}

func TestUpgradeRun(t *testing.T) {
	h := setupUpgradeHarness(t, mockCurrentSHA, mockNewSHA)
	defer h.cleanup()

	cmd, err := newTestUpgradeCommand(h.mock, []string{"-y"})
	if err != nil {
		t.Fatalf("newTestUpgradeCommand failed: %v", err)
	}

	output, err := runCaptureStdout(func() error {
		return cmd.Run()
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	expected := []string{
		"Checking for updates on branch: " + mockRefSpec,
		"Current version: " + mockCurrentSHA,
		"Fetching updates...",
		"Update Available: " + mockNewSHA,
		"Analyzing package changes...",
		"app-misc/foo-1.0 -> app-misc/foo-1.1",
		"Deploying update...",
		"Upgrade successful!",
		"Please reboot at your earliest convenience.",
	}
	for _, s := range expected {
		if !strings.Contains(output, s) {
			t.Errorf("Missing expected output: %q", s)
		}
	}
}

func TestUpgradeNoUpdate(t *testing.T) {
	h := setupUpgradeHarness(t, mockNewSHA, mockNewSHA)
	defer h.cleanup()

	// Both current and new are the same commit
	h.mock.deployments[0].Checksum = mockNewSHA

	cmd, err := newTestUpgradeCommand(h.mock, nil)
	if err != nil {
		t.Fatalf("newTestUpgradeCommand failed: %v", err)
	}

	output, err := runCaptureStdout(func() error {
		return cmd.Run()
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !strings.Contains(output, "System is already up to date") {
		t.Errorf("Expected 'System is already up to date', got:\n%s", output)
	}
}

func TestUpgradePretend(t *testing.T) {
	h := setupUpgradeHarness(t, mockCurrentSHA, mockNewSHA)
	defer h.cleanup()

	cmd, err := newTestUpgradeCommand(h.mock, []string{"--pretend"})
	if err != nil {
		t.Fatalf("newTestUpgradeCommand failed: %v", err)
	}

	output, err := runCaptureStdout(func() error {
		return cmd.Run()
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	expected := []string{
		"Fetching updates...",
		"Analyzing package changes...",
		"Running in pretend mode. Exiting.",
	}
	for _, s := range expected {
		if !strings.Contains(output, s) {
			t.Errorf("Missing expected output: %q\nGot:\n%s", s, output)
		}
	}
	if strings.Contains(output, "Deploying update...") {
		t.Error("Should not deploy in pretend mode")
	}
}

func TestUpgradeForce(t *testing.T) {
	h := setupUpgradeHarness(t, mockNewSHA, mockNewSHA)
	defer h.cleanup()

	h.mock.deployments[0].Checksum = mockNewSHA

	cmd, err := newTestUpgradeCommand(h.mock, []string{"--force", "-y"})
	if err != nil {
		t.Fatalf("newTestUpgradeCommand failed: %v", err)
	}

	output, err := runCaptureStdout(func() error {
		return cmd.Run()
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	expected := []string{
		"System is already up to date.",
		"Forcing update despite no changes...",
		"Deploying update...",
		"Upgrade successful!",
	}
	for _, s := range expected {
		if !strings.Contains(output, s) {
			t.Errorf("Missing expected output: %q\nGot:\n%s", s, output)
		}
	}
}

func TestUpgradeAbort(t *testing.T) {
	h := setupUpgradeHarness(t, mockCurrentSHA, mockNewSHA)
	defer h.cleanup()

	// Simulate user typing "n"
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		r.Close()
	}()
	go func() {
		w.Write([]byte("n\n"))
		w.Close()
	}()

	cmd, err := newTestUpgradeCommand(h.mock, nil)
	if err != nil {
		t.Fatalf("newTestUpgradeCommand failed: %v", err)
	}

	output, err := runCaptureStdout(func() error {
		return cmd.Run()
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !strings.Contains(output, "Aborted.") {
		t.Errorf("Expected 'Aborted.', got:\n%s", output)
	}
	if strings.Contains(output, "Deploying update...") {
		t.Error("Should not deploy after abort")
	}
}

func TestUpgradeBootloaderSuccess(t *testing.T) {
	h := setupUpgradeHarness(t, mockCurrentSHA, mockNewSHA)
	defer h.cleanup()

	tmpDir := t.TempDir()

	// Add a non-booted deployment for the new commit (bootloader update needs it)
	h.mock.deployments = append(h.mock.deployments, cds.Deployment{
		Booted:    false,
		Checksum:  mockNewSHA,
		Stateroot: stateroot,
		Refspec:   mockRefSpec,
		Index:     0,
	})
	h.mock.root = tmpDir

	// Create deployment rootfs with grub + shim files
	newRoot := cds.BuildDeploymentRootfs(tmpDir, stateroot, mockNewSHA, 0)
	grubSrc := filepath.Join(newRoot, "usr/lib/grub/grub-x86_64.efi.signed")
	if err := os.MkdirAll(filepath.Dir(grubSrc), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(grubSrc, []byte("new grub"), 0644); err != nil {
		t.Fatal(err)
	}
	shimDir := filepath.Join(newRoot, "usr/share/shim")
	if err := os.MkdirAll(shimDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shimDir, "shimx64.efi"), []byte("new shim"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create EFI directory with existing grub + certificate
	efiRoot := filepath.Join(tmpDir, "efi")
	grubDir := filepath.Join(efiRoot, "EFI/BOOT")
	if err := os.MkdirAll(grubDir, 0755); err != nil {
		t.Fatal(err)
	}
	existingGrub := filepath.Join(grubDir, "GRUBX64.EFI")
	if err := os.WriteFile(existingGrub, []byte("old grub"), 0644); err != nil {
		t.Fatal(err)
	}
	certFile := filepath.Join(efiRoot, "secureboot.crt")
	if err := os.WriteFile(certFile, []byte("dummy cert"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &testConfig{items: map[string]string{
		"Imager.EfiRoot":                efiRoot,
		"Imager.EfiCertificateFileName": "secureboot.crt",
	}}

	cmd, err := newTestUpgradeCommandWithConfig(h.mock, cfg, []string{"-y", "--update-bootloader"})
	if err != nil {
		t.Fatalf("newTestUpgradeCommandWithConfig failed: %v", err)
	}

	output, err := runCaptureStdout(func() error {
		return cmd.Run()
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	expected := []string{
		"Updating bootloader binaries...",
		"Found EFI file: " + existingGrub,
		"Verified EFI file: " + existingGrub,
		"Updating GRUB/Shim in " + grubDir,
		"Copying grub-x86_64.efi.signed to " + existingGrub,
		"Copying shimx64.efi to " + filepath.Join(grubDir, "shimx64.efi"),
		"Upgrade successful!",
	}
	for _, s := range expected {
		if !strings.Contains(output, s) {
			t.Errorf("Missing expected output: %q\nGot:\n%s", s, output)
		}
	}

	content, err := os.ReadFile(existingGrub)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new grub" {
		t.Errorf("Expected grub content 'new grub', got %q", content)
	}
}

func TestUpgradeBootloaderMissingConfig(t *testing.T) {
	h := setupUpgradeHarness(t, mockCurrentSHA, mockNewSHA)
	defer h.cleanup()

	cfg := &testConfig{items: map[string]string{}}

	cmd, err := newTestUpgradeCommandWithConfig(h.mock, cfg, []string{"-y", "--update-bootloader"})
	if err != nil {
		t.Fatalf("newTestUpgradeCommandWithConfig failed: %v", err)
	}

	_, err = runCaptureStdout(func() error {
		return cmd.Run()
	})
	if err == nil {
		t.Fatal("Expected error for missing EfiRoot config, got nil")
	}
	if !strings.Contains(err.Error(), "invalid key") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestUpgradeBootloaderMissingCert(t *testing.T) {
	h := setupUpgradeHarness(t, mockCurrentSHA, mockNewSHA)
	defer h.cleanup()

	efiRoot := t.TempDir()
	cfg := &testConfig{items: map[string]string{
		"Imager.EfiRoot":                efiRoot,
		"Imager.EfiCertificateFileName": "nonexistent.crt",
	}}

	cmd, err := newTestUpgradeCommandWithConfig(h.mock, cfg, []string{"-y", "--update-bootloader"})
	if err != nil {
		t.Fatalf("newTestUpgradeCommandWithConfig failed: %v", err)
	}

	_, err = runCaptureStdout(func() error {
		return cmd.Run()
	})
	if err == nil {
		t.Fatal("Expected error for missing cert, got nil")
	}
}

func TestUpgradeNotRoot(t *testing.T) {
	origEuid := getEuid
	getEuid = func() int { return 1000 }
	defer func() { getEuid = origEuid }()

	cmd := &UpgradeCommand{}
	cmd.ot = &mockOstree{}
	cmd.StartUI()
	if err := cmd.parseArgs([]string{"-y"}); err != nil {
		t.Fatalf("parseArgs failed: %v", err)
	}

	err := cmd.Run()
	if err == nil {
		t.Fatal("Expected error for non-root, got nil")
	}
	if !strings.Contains(err.Error(), "must be run as root") {
		t.Errorf("Unexpected error: %v", err)
	}
}

// --- helpers for 3-way diff tests ---

func mkPI(path, typ string, perms uint32, uid, gid, size uint64, link string) fslib.PathInfo {
	return fslib.PathInfo{
		Mode: &fslib.PathMode{Type: typ, Perms: os.FileMode(perms)},
		Uid:  uid, Gid: gid, Size: size,
		Path: path, Link: link,
	}
}

func findChange(changes []EtcChange, path string) *EtcChange {
	for i := range changes {
		if changes[i].Path == path {
			return &changes[i]
		}
	}
	return nil
}

func TestPathInfoMetaEqual(t *testing.T) {
	a := mkPI("/usr/etc/foo", "-", 0644, 0, 0, 100, "")
	b := mkPI("/etc/foo", "-", 0644, 0, 0, 100, "")
	if !pathInfoMetaEqual(&a, &b) {
		t.Error("Expected equal (path is not compared)")
	}

	// Different perms
	c := mkPI("/etc/foo", "-", 0755, 0, 0, 100, "")
	if pathInfoMetaEqual(&a, &c) {
		t.Error("Expected not equal (different perms)")
	}

	// Different size
	d := mkPI("/etc/foo", "-", 0644, 0, 0, 200, "")
	if pathInfoMetaEqual(&a, &d) {
		t.Error("Expected not equal (different size)")
	}

	// Different type
	e := mkPI("/etc/foo", "l", 0644, 0, 0, 100, "/bar")
	if pathInfoMetaEqual(&a, &e) {
		t.Error("Expected not equal (different type)")
	}

	// Symlinks with different targets
	f := mkPI("/etc/link", "l", 0777, 0, 0, 0, "target_a")
	g := mkPI("/etc/link", "l", 0777, 0, 0, 0, "target_b")
	if pathInfoMetaEqual(&f, &g) {
		t.Error("Expected not equal (different link target)")
	}
}

func TestComputeEtcDiffUnchanged(t *testing.T) {
	old := []fslib.PathInfo{mkPI("/usr/etc/passwd", "-", 0644, 0, 0, 100, "")}
	new_ := []fslib.PathInfo{mkPI("/usr/etc/passwd", "-", 0644, 0, 0, 100, "")}
	user := []*fslib.PathInfo{ptr(mkPI("/etc/passwd", "-", 0644, 0, 0, 100, ""))}

	changes := computeEtcDiff(&old, &new_, user)
	if len(changes) != 0 {
		t.Errorf("Expected no changes, got %d: %+v", len(changes), changes)
	}
}

func TestComputeEtcDiffUpstreamAdd(t *testing.T) {
	old := []fslib.PathInfo{}
	new_ := []fslib.PathInfo{mkPI("/usr/etc/newfile", "-", 0644, 0, 0, 50, "")}
	user := []*fslib.PathInfo{}

	changes := computeEtcDiff(&old, &new_, user)
	if len(changes) != 1 {
		t.Fatalf("Expected 1 change, got %d", len(changes))
	}
	c := changes[0]
	if c.Path != "newfile" || c.Action != EtcActionAdd {
		t.Errorf("Expected add of 'newfile', got %q action=%s", c.Path, c.Action)
	}
	if c.Old != nil || c.New == nil || c.User != nil {
		t.Error("Old/User should be nil, New should be set")
	}
}

func TestComputeEtcDiffUpstreamRemove(t *testing.T) {
	old := []fslib.PathInfo{mkPI("/usr/etc/gone", "-", 0644, 0, 0, 10, "")}
	new_ := []fslib.PathInfo{}
	user := []*fslib.PathInfo{ptr(mkPI("/etc/gone", "-", 0644, 0, 0, 10, ""))}

	changes := computeEtcDiff(&old, &new_, user)
	if len(changes) != 1 {
		t.Fatalf("Expected 1 change, got %d", len(changes))
	}
	c := changes[0]
	if c.Path != "gone" || c.Action != EtcActionRemove {
		t.Errorf("Expected remove of 'gone', got %q action=%s", c.Path, c.Action)
	}
}

func TestComputeEtcDiffUpstreamUpdate(t *testing.T) {
	old := []fslib.PathInfo{mkPI("/usr/etc/cfg", "-", 0644, 0, 0, 100, "")}
	new_ := []fslib.PathInfo{mkPI("/usr/etc/cfg", "-", 0644, 0, 0, 200, "")} // size changed
	user := []*fslib.PathInfo{ptr(mkPI("/etc/cfg", "-", 0644, 0, 0, 100, ""))}

	changes := computeEtcDiff(&old, &new_, user)
	if len(changes) != 1 {
		t.Fatalf("Expected 1 change, got %d", len(changes))
	}
	c := changes[0]
	if c.Path != "cfg" || c.Action != EtcActionUpdate {
		t.Errorf("Expected update of 'cfg', got %q action=%s", c.Path, c.Action)
	}
}

func TestComputeEtcDiffUserOnly(t *testing.T) {
	old := []fslib.PathInfo{mkPI("/usr/etc/cfg", "-", 0644, 0, 0, 100, "")}
	new_ := []fslib.PathInfo{mkPI("/usr/etc/cfg", "-", 0644, 0, 0, 100, "")}
	user := []*fslib.PathInfo{ptr(mkPI("/etc/cfg", "-", 0755, 0, 0, 100, ""))} // perms changed

	changes := computeEtcDiff(&old, &new_, user)
	if len(changes) != 1 {
		t.Fatalf("Expected 1 change, got %d", len(changes))
	}
	c := changes[0]
	if c.Path != "cfg" || c.Action != EtcActionUserOnly {
		t.Errorf("Expected user-only of 'cfg', got %q action=%s", c.Path, c.Action)
	}
}

func TestComputeEtcDiffConflictBothModified(t *testing.T) {
	old := []fslib.PathInfo{mkPI("/usr/etc/cfg", "-", 0644, 0, 0, 100, "")}
	new_ := []fslib.PathInfo{mkPI("/usr/etc/cfg", "-", 0644, 0, 0, 200, "")}   // upstream size change
	user := []*fslib.PathInfo{ptr(mkPI("/etc/cfg", "-", 0755, 0, 0, 300, ""))} // user perms+size change

	changes := computeEtcDiff(&old, &new_, user)
	if len(changes) != 1 {
		t.Fatalf("Expected 1 change, got %d", len(changes))
	}
	c := changes[0]
	if c.Path != "cfg" || c.Action != EtcActionConflict {
		t.Errorf("Expected conflict of 'cfg', got %q action=%s", c.Path, c.Action)
	}
}

func TestComputeEtcDiffConverged(t *testing.T) {
	// old=A, new=B, user=B → both changed the same way → skip
	old := []fslib.PathInfo{mkPI("/usr/etc/cfg", "-", 0644, 0, 0, 100, "")}
	new_ := []fslib.PathInfo{mkPI("/usr/etc/cfg", "-", 0755, 0, 0, 200, "")}
	user := []*fslib.PathInfo{ptr(mkPI("/etc/cfg", "-", 0755, 0, 0, 200, ""))}

	changes := computeEtcDiff(&old, &new_, user)
	if len(changes) != 0 {
		t.Errorf("Expected no changes (converged), got %d: %+v", len(changes), changes)
	}
}

func TestComputeEtcDiffBothRemoved(t *testing.T) {
	old := []fslib.PathInfo{mkPI("/usr/etc/gone", "-", 0644, 0, 0, 10, "")}
	new_ := []fslib.PathInfo{}
	user := []*fslib.PathInfo{}

	changes := computeEtcDiff(&old, &new_, user)
	if len(changes) != 0 {
		t.Errorf("Expected no changes (both removed), got %d", len(changes))
	}
}

func TestComputeEtcDiffConflictUpstreamRemoveUserModified(t *testing.T) {
	old := []fslib.PathInfo{mkPI("/usr/etc/cfg", "-", 0644, 0, 0, 100, "")}
	new_ := []fslib.PathInfo{}
	user := []*fslib.PathInfo{ptr(mkPI("/etc/cfg", "-", 0755, 0, 0, 100, ""))} // user changed perms

	changes := computeEtcDiff(&old, &new_, user)
	if len(changes) != 1 {
		t.Fatalf("Expected 1 change, got %d", len(changes))
	}
	if changes[0].Action != EtcActionConflict {
		t.Errorf("Expected conflict, got %s", changes[0].Action)
	}
}

func TestComputeEtcDiffConflictUpstreamChangedUserRemoved(t *testing.T) {
	old := []fslib.PathInfo{mkPI("/usr/etc/cfg", "-", 0644, 0, 0, 100, "")}
	new_ := []fslib.PathInfo{mkPI("/usr/etc/cfg", "-", 0644, 0, 0, 200, "")} // upstream changed
	user := []*fslib.PathInfo{}                                              // user removed

	changes := computeEtcDiff(&old, &new_, user)
	if len(changes) != 1 {
		t.Fatalf("Expected 1 change, got %d", len(changes))
	}
	if changes[0].Action != EtcActionConflict {
		t.Errorf("Expected conflict, got %s", changes[0].Action)
	}
}

func TestComputeEtcDiffUserRemovedUnchangedUpstream(t *testing.T) {
	old := []fslib.PathInfo{mkPI("/usr/etc/cfg", "-", 0644, 0, 0, 100, "")}
	new_ := []fslib.PathInfo{mkPI("/usr/etc/cfg", "-", 0644, 0, 0, 100, "")} // unchanged
	user := []*fslib.PathInfo{}                                              // user removed

	changes := computeEtcDiff(&old, &new_, user)
	if len(changes) != 1 {
		t.Fatalf("Expected 1 change, got %d", len(changes))
	}
	if changes[0].Action != EtcActionUserOnly {
		t.Errorf("Expected user-only, got %s", changes[0].Action)
	}
}

func TestComputeEtcDiffUserAdded(t *testing.T) {
	old := []fslib.PathInfo{}
	new_ := []fslib.PathInfo{}
	user := []*fslib.PathInfo{ptr(mkPI("/etc/custom", "-", 0644, 0, 0, 42, ""))}

	changes := computeEtcDiff(&old, &new_, user)
	if len(changes) != 1 {
		t.Fatalf("Expected 1 change, got %d", len(changes))
	}
	c := changes[0]
	if c.Path != "custom" || c.Action != EtcActionUserOnly {
		t.Errorf("Expected user-only of 'custom', got %q action=%s", c.Path, c.Action)
	}
}

func TestComputeEtcDiffConflictBothAdded(t *testing.T) {
	old := []fslib.PathInfo{}
	new_ := []fslib.PathInfo{mkPI("/usr/etc/both", "-", 0644, 0, 0, 50, "")}
	user := []*fslib.PathInfo{ptr(mkPI("/etc/both", "-", 0755, 0, 0, 60, ""))} // different

	changes := computeEtcDiff(&old, &new_, user)
	if len(changes) != 1 {
		t.Fatalf("Expected 1 change, got %d", len(changes))
	}
	if changes[0].Action != EtcActionConflict {
		t.Errorf("Expected conflict, got %s", changes[0].Action)
	}
}

func TestComputeEtcDiffBothAddedIdentical(t *testing.T) {
	old := []fslib.PathInfo{}
	new_ := []fslib.PathInfo{mkPI("/usr/etc/same", "-", 0644, 0, 0, 50, "")}
	user := []*fslib.PathInfo{ptr(mkPI("/etc/same", "-", 0644, 0, 0, 50, ""))}

	changes := computeEtcDiff(&old, &new_, user)
	if len(changes) != 0 {
		t.Errorf("Expected no changes (both added identical), got %d", len(changes))
	}
}

func TestComputeEtcDiffSymlinks(t *testing.T) {
	old := []fslib.PathInfo{mkPI("/usr/etc/link", "l", 0777, 0, 0, 0, "old_target")}
	new_ := []fslib.PathInfo{mkPI("/usr/etc/link", "l", 0777, 0, 0, 0, "new_target")}
	user := []*fslib.PathInfo{ptr(mkPI("/etc/link", "l", 0777, 0, 0, 0, "old_target"))}

	changes := computeEtcDiff(&old, &new_, user)
	if len(changes) != 1 {
		t.Fatalf("Expected 1 change, got %d", len(changes))
	}
	c := changes[0]
	if c.Path != "link" || c.Action != EtcActionUpdate {
		t.Errorf("Expected update of symlink 'link', got %q action=%s", c.Path, c.Action)
	}
}

func TestComputeEtcDiffMultipleChanges(t *testing.T) {
	old := []fslib.PathInfo{
		mkPI("/usr/etc/keep", "-", 0644, 0, 0, 100, ""),
		mkPI("/usr/etc/update", "-", 0644, 0, 0, 100, ""),
		mkPI("/usr/etc/conflict", "-", 0644, 0, 0, 100, ""),
		mkPI("/usr/etc/remove", "-", 0644, 0, 0, 100, ""),
	}
	new_ := []fslib.PathInfo{
		mkPI("/usr/etc/keep", "-", 0644, 0, 0, 100, ""),
		mkPI("/usr/etc/update", "-", 0644, 0, 0, 200, ""),   // upstream changed size
		mkPI("/usr/etc/conflict", "-", 0644, 0, 0, 300, ""), // upstream changed
		mkPI("/usr/etc/added", "-", 0644, 0, 0, 50, ""),     // new file
	}
	user := []*fslib.PathInfo{
		ptr(mkPI("/etc/keep", "-", 0644, 0, 0, 100, "")),
		ptr(mkPI("/etc/update", "-", 0644, 0, 0, 100, "")),   // unchanged
		ptr(mkPI("/etc/conflict", "-", 0755, 0, 0, 400, "")), // user also changed
		ptr(mkPI("/etc/remove", "-", 0644, 0, 0, 100, "")),   // upstream removed, user unchanged
		ptr(mkPI("/etc/useronly", "-", 0644, 0, 0, 99, "")),  // user added
	}

	changes := computeEtcDiff(&old, &new_, user)

	expected := map[string]EtcChangeAction{
		"update":   EtcActionUpdate,
		"conflict": EtcActionConflict,
		"added":    EtcActionAdd,
		"remove":   EtcActionRemove,
		"useronly": EtcActionUserOnly,
	}

	if len(changes) != len(expected) {
		t.Fatalf("Expected %d changes, got %d: %+v", len(expected), len(changes), changes)
	}
	for path, action := range expected {
		c := findChange(changes, path)
		if c == nil {
			t.Errorf("Missing change for path %q", path)
			continue
		}
		if c.Action != action {
			t.Errorf("Path %q: expected action %s, got %s", path, action, c.Action)
		}
	}
}

func TestComputeEtcDiffNilInputs(t *testing.T) {
	// nil old and new should not panic
	user := []*fslib.PathInfo{ptr(mkPI("/etc/custom", "-", 0644, 0, 0, 10, ""))}
	changes := computeEtcDiff(nil, nil, user)
	if len(changes) != 1 {
		t.Fatalf("Expected 1 change, got %d", len(changes))
	}
	if changes[0].Action != EtcActionUserOnly {
		t.Errorf("Expected user-only, got %s", changes[0].Action)
	}
}

func TestComputeEtcDiffSorted(t *testing.T) {
	old := []fslib.PathInfo{}
	new_ := []fslib.PathInfo{
		mkPI("/usr/etc/z_file", "-", 0644, 0, 0, 1, ""),
		mkPI("/usr/etc/a_file", "-", 0644, 0, 0, 1, ""),
		mkPI("/usr/etc/m_file", "-", 0644, 0, 0, 1, ""),
	}
	user := []*fslib.PathInfo{}

	changes := computeEtcDiff(&old, &new_, user)
	if len(changes) != 3 {
		t.Fatalf("Expected 3 changes, got %d", len(changes))
	}
	if changes[0].Path != "a_file" || changes[1].Path != "m_file" || changes[2].Path != "z_file" {
		t.Errorf("Results not sorted: %s, %s, %s",
			changes[0].Path, changes[1].Path, changes[2].Path)
	}
}

func TestComputeEtcDiffDirectories(t *testing.T) {
	old := []fslib.PathInfo{mkPI("/usr/etc/conf.d", "d", 0755, 0, 0, 0, "")}
	new_ := []fslib.PathInfo{mkPI("/usr/etc/conf.d", "d", 0700, 0, 0, 0, "")} // perms changed
	user := []*fslib.PathInfo{ptr(mkPI("/etc/conf.d", "d", 0755, 0, 0, 0, ""))}

	changes := computeEtcDiff(&old, &new_, user)
	if len(changes) != 1 {
		t.Fatalf("Expected 1 change, got %d", len(changes))
	}
	c := changes[0]
	if c.Path != "conf.d" || c.Action != EtcActionUpdate {
		t.Errorf("Expected update of directory 'conf.d', got %q action=%s", c.Path, c.Action)
	}
}

func ptr(pi fslib.PathInfo) *fslib.PathInfo {
	return &pi
}
