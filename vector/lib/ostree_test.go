package ostree

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// MockConfig implements config.IConfig for testing
type MockConfig struct {
	Items map[string][]string
	Bools map[string]bool
}

func (m *MockConfig) Load() error {
	return nil
}

func (m *MockConfig) GetItem(key string) (string, error) {
	if val, ok := m.Items[key]; ok {
		return val[0], nil
	}
	return "", nil // Return empty string for not found to simulate empty config
}

func (m *MockConfig) GetItems(key string) ([]string, error) {
	if val, ok := m.Items[key]; ok {
		return val, nil
	}
	return nil, nil // Return empty slice for not found to simulate empty config
}

func (m *MockConfig) GetBool(key string) (bool, error) {
	if val, ok := m.Bools[key]; ok {
		return val, nil
	}
	return false, nil
}

func TestBranchHelpers(t *testing.T) {
	tests := []struct {
		name     string
		ref      string
		remote   string
		cleanRef string
		isShort  bool
	}{
		{"Full Ref", "origin:matrixos/dev", "origin", "matrixos/dev", false},
		{"Short Ref", "gnome", "", "gnome", true},
		{"Ref No Remote", "matrixos/dev", "", "matrixos/dev", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractRemoteFromRef(tt.ref); got != tt.remote {
				t.Errorf("ExtractRemoteFromRef(%q) = %q, want %q", tt.ref, got, tt.remote)
			}
			if got := CleanRemoteFromRef(tt.ref); got != tt.cleanRef {
				t.Errorf("CleanRemoteFromRef(%q) = %q, want %q", tt.ref, got, tt.cleanRef)
			}
			if got := IsBranchShortName(tt.cleanRef); got != tt.isShort {
				t.Errorf("IsBranchShortName(%q) = %v, want %v", tt.cleanRef, got, tt.isShort)
			}
		})
	}
}

func TestBranchShortnameToNormal(t *testing.T) {
	got, err := BranchShortnameToNormal("dev", "gnome", "matrixos", "amd64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "matrixos/amd64/dev/gnome"
	if got != want {
		t.Errorf("BranchShortnameToNormal = %q, want %q", got, want)
	}

	gotProd, err := BranchShortnameToNormal("prod", "gnome", "matrixos", "amd64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantProd := "matrixos/amd64/gnome"
	if gotProd != wantProd {
		t.Errorf("BranchShortnameToNormal(prod) = %q, want %q", gotProd, wantProd)
	}
}

func checkOstreeAvailable(t *testing.T) {
	_, err := exec.LookPath("ostree")
	if err != nil {
		t.Skip("ostree binary not found, skipping integration tests")
	}
}

func setupTestRepo(t *testing.T) string {
	checkOstreeAvailable(t)
	dir := t.TempDir()

	// Initialize repo
	cmd := exec.Command("ostree", "init", "--repo="+dir, "--mode=archive")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to init ostree repo: %v, output: %s", err, out)
	}
	return dir
}

func TestRepoOperations(t *testing.T) {
	repoDir := setupTestRepo(t)

	// Test ListRemotes (empty)
	remotes, err := ListRemotes(repoDir, false)
	if err != nil {
		t.Fatalf("ListRemotes failed: %v", err)
	}
	if len(remotes) != 0 {
		t.Errorf("expected 0 remotes, got %d", len(remotes))
	}

	// Test AddRemote
	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.RepoDir":   {repoDir},
			"Ostree.Remote":    {"origin"},
			"Ostree.RemoteUrl": {"http://example.com"},
		},
		Bools: map[string]bool{
			"Ostree.Gpg": false,
		},
	}
	o, _ := New(cfg)

	err = o.AddRemote(false)
	if err != nil {
		t.Fatalf("AddRemote failed: %v", err)
	}

	// Test ListRemotes (1)
	remotes, err = ListRemotes(repoDir, false)
	if err != nil {
		t.Fatalf("ListRemotes failed: %v", err)
	}
	if len(remotes) != 1 || remotes[0] != "origin" {
		t.Errorf("expected [origin], got %v", remotes)
	}

	// Test ListLocalRefs (empty)
	refs, err := ListLocalRefs(repoDir, false)
	if err != nil {
		t.Fatalf("ListLocalRefs failed: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("expected 0 refs, got %d", len(refs))
	}
}

func TestCommitAndListPackages(t *testing.T) {
	repoDir := setupTestRepo(t)

	// Create content to commit
	contentDir := t.TempDir()

	// Create package structure: var/db/pkg/sys-apps/systemd
	pkgDir := filepath.Join(contentDir, "var", "db", "pkg", "sys-apps", "systemd")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Create a file inside
	if err := os.WriteFile(filepath.Join(pkgDir, "CONTENTS"), []byte("foo"), 0644); err != nil {
		t.Fatal(err)
	}

	// Commit
	branch := "test/branch"
	cmd := exec.Command("ostree", "commit", "--repo="+repoDir, "--branch="+branch, "--subject=test", contentDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ostree commit failed: %v, output: %s", err, out)
	}

	// Get Commit Hash
	commit, err := LastCommit(repoDir, branch, false)
	if err != nil {
		t.Fatalf("LastCommit failed: %v", err)
	}
	if commit == "" {
		t.Fatal("commit hash is empty")
	}

	// Setup Ostree struct
	cfg := &MockConfig{
		Items: map[string][]string{
			"Releaser.ReadOnlyVdb": {"/var/db/pkg"},
		},
	}
	o, _ := New(cfg)

	// Create a fake sysroot structure because ListPackages expects sysroot/ostree/repo
	sysroot := t.TempDir()
	sysrootRepo := filepath.Join(sysroot, "ostree", "repo")
	if err := os.MkdirAll(filepath.Dir(sysrootRepo), 0755); err != nil {
		t.Fatal(err)
	}
	// Symlink the repo we created to the sysroot location
	if err := os.Symlink(repoDir, sysrootRepo); err != nil {
		t.Fatal(err)
	}
	// Also create the vdb dir in sysroot as ListPackages checks for existence
	if err := os.MkdirAll(filepath.Join(sysroot, "var", "db", "pkg"), 0755); err != nil {
		t.Fatal(err)
	}

	pkgs, err := o.ListPackages(commit, sysroot, false)
	if err != nil {
		t.Fatalf("ListPackages failed: %v", err)
	}

	if len(pkgs) != 1 {
		t.Errorf("expected 1 package, got %d", len(pkgs))
	} else if pkgs[0] != "sys-apps/systemd" {
		t.Errorf("expected sys-apps/systemd, got %s", pkgs[0])
	}
}

func TestPrepareFilesystemHierarchy(t *testing.T) {
	imageDir := t.TempDir()

	// Create initial directories that are expected to exist or be moved
	dirs := []string{
		"tmp",
		"etc",
		"var/db/pkg",
		"opt",
		"srv",
		"home",
		"usr/local",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(imageDir, d), 0755); err != nil {
			t.Fatal(err)
		}
	}
	// Create machine-id
	if err := os.WriteFile(filepath.Join(imageDir, "etc", "machine-id"), []byte("dummy"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &MockConfig{
		Items: map[string][]string{
			"Releaser.ReadOnlyVdb": {"/usr/var/db/pkg"}, // Different to test move
			"Imager.EfiRoot":       {"/efi"},
		},
	}
	o, _ := New(cfg)

	if err := o.PrepareFilesystemHierarchy(imageDir); err != nil {
		t.Fatalf("PrepareFilesystemHierarchy failed: %v", err)
	}

	// Verifications
	assertSymlink(t, filepath.Join(imageDir, "ostree"), "sysroot/ostree")
	assertSymlink(t, filepath.Join(imageDir, "tmp"), "sysroot/tmp")
	assertDir(t, filepath.Join(imageDir, "sysroot", "tmp"))

	assertDir(t, filepath.Join(imageDir, "usr", "etc"))
	// Note: PrepareFilesystemHierarchy moves etc -> usr/etc but does NOT create the symlink back.
	// That is handled by a separate function in the bash script (release_lib.symlink_etc).
	if _, err := os.Stat(filepath.Join(imageDir, "etc")); !os.IsNotExist(err) {
		t.Error("etc directory should have been moved")
	}

	// Check var/db/pkg move
	// Config was /usr/var/db/pkg
	assertDir(t, filepath.Join(imageDir, "usr", "var", "db", "pkg"))
	assertSymlink(t, filepath.Join(imageDir, "var", "db", "pkg"), "../../usr/var/db/pkg")

	// Check opt
	assertDir(t, filepath.Join(imageDir, "usr", "opt"))
	assertSymlink(t, filepath.Join(imageDir, "opt"), "usr/opt")

	// Check srv
	assertDir(t, filepath.Join(imageDir, "var", "srv"))
	assertSymlink(t, filepath.Join(imageDir, "srv"), "var/srv")

	// Check home
	assertDir(t, filepath.Join(imageDir, "var", "home"))
	assertSymlink(t, filepath.Join(imageDir, "home"), "var/home")

	// Check usr/local
	assertDir(t, filepath.Join(imageDir, "var", "usrlocal"))
	assertSymlink(t, filepath.Join(imageDir, "usr", "local"), "../var/usrlocal")
}

func assertSymlink(t *testing.T, path, target string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Errorf("stat %s failed: %v", path, err)
		return
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("%s is not a symlink", path)
		return
	}
	got, err := os.Readlink(path)
	if err != nil {
		t.Errorf("readlink %s failed: %v", path, err)
		return
	}
	if got != target {
		t.Errorf("symlink %s -> %s, want %s", path, got, target)
	}
}

func assertDir(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Errorf("stat %s failed: %v", path, err)
		return
	}
	if !info.IsDir() {
		t.Errorf("%s is not a directory", path)
	}
}

func TestDeploy(t *testing.T) {
	// Save original runCommand and restore after test
	origRunCommand := runCommand
	defer func() { runCommand = origRunCommand }()

	var commands [][]string
	fakeCommit := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	// Mock runCommand
	runCommand = func(stdout, stderr io.Writer, name string, args ...string) error {
		// Capture the command
		cmdArgs := append([]string{name}, args...)
		commands = append(commands, cmdArgs)

		// Handle specific commands that need output
		// args[0] is usually the ostree subcommand if name is "ostree"
		if len(args) > 0 {
			if args[0] == "rev-parse" {
				// LastCommit expects a hash
				stdout.Write([]byte(fakeCommit + "\n"))
			}
		}
		return nil
	}

	sysroot := t.TempDir()
	repoDir := "/fake/repo"
	ref := "matrixos/dev/gnome"
	bootArgs := []string{"arg1=val1", "arg2=val2"}

	// Setup config
	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.RepoDir":  {repoDir},
			"Ostree.Sysroot":  {sysroot},
			"Ostree.Remote":   {"origin"},
			"matrixOS.OsName": {"matrixos"},
		},
	}
	o, _ := New(cfg)

	// Call Deploy
	err := o.Deploy(ref, bootArgs, false)
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}

	// Verify commands
	expectedCommands := []string{
		fmt.Sprintf("ostree rev-parse --repo=%s %s", repoDir, ref),
		fmt.Sprintf("ostree admin init-fs %s", sysroot),
		fmt.Sprintf("ostree admin os-init matrixos --sysroot=%s", sysroot),
		fmt.Sprintf("ostree pull-local --repo=%s/ostree/repo %s %s", sysroot, repoDir, fakeCommit),
		fmt.Sprintf("ostree refs --repo=%s/ostree/repo --create=origin:%s %s", sysroot, ref, fakeCommit),
		fmt.Sprintf("ostree config --repo=%s/ostree/repo set sysroot.bootloader none", sysroot),
		fmt.Sprintf("ostree config --repo=%s/ostree/repo set sysroot.bootprefix false", sysroot),
		fmt.Sprintf("ostree admin deploy --sysroot=%s --os=matrixos --karg-append=arg1=val1 --karg-append=arg2=val2 origin:%s", sysroot, ref),
	}

	if len(commands) != len(expectedCommands) {
		t.Errorf("Expected %d commands, got %d", len(expectedCommands), len(commands))
	}

	for i, cmd := range commands {
		if i >= len(expectedCommands) {
			break
		}
		cmdStr := strings.Join(cmd, " ")
		if cmdStr != expectedCommands[i] {
			t.Errorf("Command %d mismatch:\nGot:  %s\nWant: %s", i, cmdStr, expectedCommands[i])
		}
	}
}

func TestDeployIntegration(t *testing.T) {
	checkOstreeAvailable(t)
	if os.Getuid() != 0 {
		t.Skip("Skipping Deploy integration test: requires root privileges")
	}

	// Ensure we are using the real runCommand (in case other tests mocked it)
	// Since tests run sequentially in this package, this is just a safety measure.
	// Note: runCommand is a global variable in ostree.go

	repoDir := setupTestRepo(t)

	// Create content to commit
	contentDir := t.TempDir()
	// Create a minimal rootfs structure
	if err := os.MkdirAll(filepath.Join(contentDir, "usr", "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	// ostree admin deploy requires /usr/etc to exist in the commit
	if err := os.MkdirAll(filepath.Join(contentDir, "usr", "etc"), 0755); err != nil {
		t.Fatal(err)
	}
	// Create a dummy kernel in /usr/lib/modules/KVER/vmlinuz
	// ostree admin deploy expects the kernel to be in /usr/lib/modules or /boot (with specific naming)
	kernelVer := "6.6.6-test"
	modulesDir := filepath.Join(contentDir, "usr", "lib", "modules", kernelVer)
	if err := os.MkdirAll(modulesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modulesDir, "vmlinuz"), []byte("kernel"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modulesDir, "initramfs.img"), []byte("initramfs"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contentDir, "usr", "lib", "os-release"), []byte("NAME=TestOS\nID=testos\nVERSION=1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../lib/os-release", filepath.Join(contentDir, "usr/etc", "os-release")); err != nil {
		t.Fatal(err)
	}

	branch := "test/os"
	// Commit to the repo
	cmd := exec.Command("ostree", "commit", "--repo="+repoDir, "--branch="+branch, "--subject=test", contentDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ostree commit failed: %v, output: %s", err, out)
	}

	// Setup config for the Deploy call
	sysroot := t.TempDir()
	// ostree admin deploy sets immutable attributes on the deployment directory.
	// We need to clear them to allow t.TempDir cleanup to succeed.
	defer func() {
		exec.Command("chattr", "-R", "-i", sysroot).Run()
	}()
	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.RepoDir":  {repoDir},
			"Ostree.Sysroot":  {sysroot},
			"Ostree.Remote":   {"origin"},
			"matrixOS.OsName": {"matrixos"},
		},
	}
	o, _ := New(cfg)

	// Perform Deployment
	// This will pull from repoDir into sysroot/ostree/repo and then deploy
	if err := o.Deploy(branch, []string{"karg1=val1"}, false); err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}

	// Verify that the deployment directory was created
	// We can verify the booted ref or just check if the deployment directory exists
	if _, err := o.DeployedRootfs(branch, false); err != nil {
		t.Errorf("DeployedRootfs failed or deployment not found: %v", err)
	}
}
