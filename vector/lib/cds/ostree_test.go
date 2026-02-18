package cds

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
	if lst, ok := m.Items[key]; ok {
		var val string
		if len(lst) > 0 {
			val = lst[len(lst)-1]
		}
		return val, nil
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
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

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
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

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
		"root",
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
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

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
	// Check root
	assertDir(t, filepath.Join(imageDir, "var", "roothome"))
	assertSymlink(t, filepath.Join(imageDir, "root"), "var/roothome")

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
	var commands [][]string
	fakeCommit := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

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
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
		cmdArgs := append([]string{name}, args...)
		commands = append(commands, cmdArgs)

		if len(args) > 0 {
			if args[0] == "rev-parse" {
				stdout.Write([]byte(fakeCommit + "\n"))
			}
		}
		return nil
	}

	// Call Deploy
	err = o.Deploy(ref, bootArgs, false)
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
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

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

func TestBranchContainsRemote(t *testing.T) {
	tests := []struct {
		branch string
		want   bool
	}{
		{"origin:branch", true},
		{"branch", false},
		{"remote:group/branch", true},
	}
	for _, tt := range tests {
		if got := BranchContainsRemote(tt.branch); got != tt.want {
			t.Errorf("BranchContainsRemote(%q) = %v, want %v", tt.branch, got, tt.want)
		}
	}
}

func TestFullBranchHelpers(t *testing.T) {
	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.FullBranchSuffix": {"full"},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	// IsBranchFullSuffixed
	if isFull, _ := o.IsBranchFullSuffixed("branch-full"); !isFull {
		t.Error("IsBranchFullSuffixed(branch-full) = false, want true")
	}
	if isFull, _ := o.IsBranchFullSuffixed("branch"); isFull {
		t.Error("IsBranchFullSuffixed(branch) = true, want false")
	}

	// BranchToFull
	if full, _ := o.BranchToFull("branch"); full != "branch-full" {
		t.Errorf("BranchToFull(branch) = %q, want branch-full", full)
	}
	if full, _ := o.BranchToFull("branch-full"); full != "branch-full" {
		t.Errorf("BranchToFull(branch-full) = %q, want branch-full", full)
	}

	// RemoveFullFromBranch
	if clean, _ := o.RemoveFullFromBranch("branch-full"); clean != "branch" {
		t.Errorf("RemoveFullFromBranch(branch-full) = %q, want branch", clean)
	}
	if clean, _ := o.RemoveFullFromBranch("branch"); clean != "branch" {
		t.Errorf("RemoveFullFromBranch(branch) = %q, want branch", clean)
	}

	// BranchShortnameToFull
	fullRef, err := o.BranchShortnameToFull("gnome", "dev", "matrixos", "amd64")
	if err != nil {
		t.Errorf("BranchShortnameToFull failed: %v", err)
	}
	wantFullRef := "matrixos/amd64/dev/gnome-full"
	if fullRef != wantFullRef {
		t.Errorf("BranchShortnameToFull = %q, want %q", fullRef, wantFullRef)
	}
}

func TestClientSideGpgArgs(t *testing.T) {
	// Test standalone function
	args, _ := ClientSideGpgArgs(false, "")
	if len(args) != 1 || args[0] != "--no-gpg-verify" {
		t.Errorf("ClientSideGpgArgs(false) = %v, want [--no-gpg-verify]", args)
	}

	args, _ = ClientSideGpgArgs(true, "/path/to/key")
	if len(args) != 2 || args[0] != "--set=gpg-verify=true" || args[1] != "--gpg-import=/path/to/key" {
		t.Errorf("ClientSideGpgArgs(true) = %v", args)
	}
}

func TestCollectionIDArgs(t *testing.T) {
	args, _ := CollectionIDArgs("org.example.Collection")
	if len(args) != 1 || args[0] != "--collection-id=org.example.Collection" {
		t.Errorf("CollectionIDArgs = %v", args)
	}

	// Test empty (error expected based on implementation)
	_, err := CollectionIDArgs("")
	if err == nil {
		t.Error("CollectionIDArgs(\"\") expected error, got nil")
	}
}

func TestOstreeCommandsMocked(t *testing.T) {
	var lastCmdArgs []string

	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.RepoDir":                {"/repo"},
			"Ostree.Root":                   {"/"},
			"Ostree.KeepObjectsYoungerThan": {"2023-01-01"},
		},
		Bools: map[string]bool{
			"Ostree.Gpg": false,
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
		lastCmdArgs = args
		// Mock rev-parse for GenerateStaticDelta
		if len(args) > 0 && args[0] == "rev-parse" {
			stdout.Write([]byte("commit-hash\n"))
		}
		return nil
	}

	// Pull
	if err := o.Pull("origin:ref", false); err != nil {
		t.Fatalf("Pull failed: %v", err)
	}
	if lastCmdArgs[1] != "pull" || lastCmdArgs[2] != "origin" || lastCmdArgs[3] != "ref" {
		t.Errorf("Pull args mismatch: %v", lastCmdArgs)
	}

	// Prune
	if err := o.Prune("ref", false); err != nil {
		t.Fatalf("Prune failed: %v", err)
	}
	// args: --repo=/repo prune --depth=5 --refs-only --keep-younger-than=... --only-branch=ref
	if lastCmdArgs[1] != "prune" || lastCmdArgs[5] != "--only-branch=ref" {
		t.Errorf("Prune args mismatch: %v", lastCmdArgs)
	}

	// GenerateStaticDelta
	if err := o.GenerateStaticDelta("ref", false); err != nil {
		t.Fatalf("GenerateStaticDelta failed: %v", err)
	}
	// First it calls rev-parse, then static-delta generate
	// Since we only capture last, we check static-delta
	if lastCmdArgs[1] != "static-delta" || lastCmdArgs[2] != "generate" {
		t.Errorf("GenerateStaticDelta args mismatch: %v", lastCmdArgs)
	}

	// UpdateSummary
	if err := o.UpdateSummary(false); err != nil {
		t.Fatalf("UpdateSummary failed: %v", err)
	}
	if lastCmdArgs[1] != "summary" || lastCmdArgs[2] != "--update" {
		t.Errorf("UpdateSummary args mismatch: %v", lastCmdArgs)
	}

	// Upgrade
	if err := o.Upgrade([]string{"--check"}, false); err != nil {
		t.Fatalf("Upgrade failed: %v", err)
	}
	if lastCmdArgs[1] != "upgrade" || lastCmdArgs[3] != "--check" {
		t.Errorf("Upgrade args mismatch: %v", lastCmdArgs)
	}
}

func TestBootedStatus(t *testing.T) {
	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.Root": {"/"},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
		// Mock ostree admin status --json
		jsonOutput := `{
			"deployments": [
				{
					"booted": true,
					"checksum": "hash123",
					"refspec": "origin:branch"
				},
				{
					"booted": false,
					"checksum": "hash456",
					"refspec": "origin:old"
				}
			]
		}`
		stdout.Write([]byte(jsonOutput))
		return nil
	}

	ref, err := o.BootedRef(false)
	if err != nil {
		t.Fatalf("BootedRef failed: %v", err)
	}
	if ref != "origin:branch" {
		t.Errorf("BootedRef = %q, want origin:branch", ref)
	}

	hash, err := o.BootedHash(false)
	if err != nil {
		t.Fatalf("BootedHash failed: %v", err)
	}
	if hash != "hash123" {
		t.Errorf("BootedHash = %q, want hash123", hash)
	}
}

func TestSetupEnvironment(t *testing.T) {
	os.Unsetenv("LC_TIME")
	SetupEnvironment()
	if got := os.Getenv("LC_TIME"); got != "C" {
		t.Errorf("LC_TIME = %q, want C", got)
	}
}

func TestGpgHelpers(t *testing.T) {
	if got := GpgSignedFilePath("file"); got != "file.asc" {
		t.Errorf("GpgSignedFilePath(file) = %q, want file.asc", got)
	}
}

func TestPatchGpgHomeDir(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("Skipping TestPatchGpgHomeDir: requires root privileges for chown")
	}
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "gpg-home")

	if err := PatchGpgHomeDir(homeDir); err != nil {
		t.Fatalf("PatchGpgHomeDir failed: %v", err)
	}

	info, err := os.Stat(homeDir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Errorf("homeDir perm = %v, want 0700", info.Mode().Perm())
	}
}

func TestGpgKeyID(t *testing.T) {
	tmpDir := t.TempDir()
	pubKey := filepath.Join(tmpDir, "pub.key")
	if err := os.WriteFile(pubKey, []byte("dummy"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.DevGpgHomedir": {filepath.Join(tmpDir, "gpg")},
			"Ostree.GpgPublicKey":  {pubKey},
		},
	}

	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
		// Mock gpg output
		// Format: pub:u:4096:1:3260D9CC6D9275DD:1678752000:::u:::scESC:
		fmt.Fprintln(stdout, "pub:u:4096:1:3260D9CC6D9275DD:1678752000:::u:::scESC:")
		return nil
	}

	keyID, err := o.GpgKeyID()
	if err != nil {
		t.Fatalf("GpgKeyID failed: %v", err)
	}
	if keyID != "3260D9CC6D9275DD" {
		t.Errorf("GpgKeyID = %q, want 3260D9CC6D9275DD", keyID)
	}
}

func TestBootCommit(t *testing.T) {
	sysroot := t.TempDir()
	osName := "matrixos"

	cfg := &MockConfig{
		Items: map[string][]string{
			"matrixOS.OsName": {osName},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	// Setup directory structure: sysroot/ostree/boot.1/matrixos/COMMIT_HASH
	bootDir := filepath.Join(sysroot, "ostree", "boot.1", osName)
	if err := os.MkdirAll(bootDir, 0755); err != nil {
		t.Fatal(err)
	}

	commitHash := "a1b2c3d4"
	if err := os.Mkdir(filepath.Join(bootDir, commitHash), 0755); err != nil {
		t.Fatal(err)
	}

	got, err := o.BootCommit(sysroot)
	if err != nil {
		t.Fatalf("BootCommit failed: %v", err)
	}
	if got != commitHash {
		t.Errorf("BootCommit = %q, want %q", got, commitHash)
	}
}

func TestMaybeInitializeRemote(t *testing.T) {
	var cmds []string
	repoDir := t.TempDir()
	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.RepoDir":   {repoDir},
			"Ostree.Remote":    {"origin"},
			"Ostree.RemoteUrl": {"http://url"},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
		cmds = append(cmds, strings.Join(args, " "))
		return nil
	}

	if err := o.MaybeInitializeRemote(false); err != nil {
		t.Fatalf("MaybeInitializeRemote failed: %v", err)
	}

	// Check for expected commands
	// 1. init (since repoDir is empty)
	// 2. remote add (since list returns empty in mock)
	if len(cmds) < 2 {
		t.Errorf("Expected at least 2 commands, got %d: %v", len(cmds), cmds)
	}
}

func TestRemoteRefs(t *testing.T) {
	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.RepoDir": {"/repo"},
			"Ostree.Remote":  {"origin"},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
		// Mock ostree remote refs output
		fmt.Fprintln(stdout, "matrixos/dev/gnome")
		fmt.Fprintln(stdout, "matrixos/prod/server")
		return nil
	}

	refs, err := o.RemoteRefs(false)
	if err != nil {
		t.Fatalf("RemoteRefs failed: %v", err)
	}
	if len(refs) != 2 {
		t.Errorf("Expected 2 refs, got %d", len(refs))
	}
	if refs[0] != "matrixos/dev/gnome" {
		t.Errorf("Unexpected ref: %s", refs[0])
	}
}

func TestAddRemoteWithSysroot(t *testing.T) {
	var lastArgs []string
	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.Remote":    {"origin"},
			"Ostree.RemoteUrl": {"http://url"},
		},
		Bools: map[string]bool{"Ostree.Gpg": false},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
		lastArgs = args
		return nil
	}

	if err := o.AddRemoteWithSysroot("/sysroot", false); err != nil {
		t.Fatalf("AddRemoteWithSysroot failed: %v", err)
	}

	// Expected: remote add --sysroot=/sysroot --force --no-gpg-verify origin http://url
	foundSysroot := false
	for _, arg := range lastArgs {
		if arg == "--sysroot=/sysroot" {
			foundSysroot = true
			break
		}
	}
	if !foundSysroot {
		t.Errorf("AddRemoteWithSysroot args missing sysroot: %v", lastArgs)
	}
}

func TestLastCommitWithSysroot(t *testing.T) {
	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.Sysroot": {"/sysroot"},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
		fmt.Fprintln(stdout, "hash123")
		return nil
	}

	hash, err := o.LastCommitWithSysroot("ref", false)
	if err != nil {
		t.Fatalf("LastCommitWithSysroot failed: %v", err)
	}
	if hash != "hash123" {
		t.Errorf("LastCommitWithSysroot = %q, want hash123", hash)
	}
}

func TestGpgSignFile(t *testing.T) {
	var cmds []string
	tmpDir := t.TempDir()
	dummyFile := filepath.Join(tmpDir, "file.txt")
	if err := os.WriteFile(dummyFile, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	pubKey := filepath.Join(tmpDir, "pub.key")
	if err := os.WriteFile(pubKey, []byte("key"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.DevGpgHomedir": {filepath.Join(tmpDir, "gpg")},
			"Ostree.GpgPublicKey":  {pubKey},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
		cmds = append(cmds, strings.Join(args, " "))
		// Mock GpgKeyID call
		if len(args) > 0 && args[0] == "--homedir" {
			// Check if it's the --show-keys call
			for _, arg := range args {
				if arg == "--show-keys" {
					fmt.Fprintln(stdout, "pub:u:4096:1:KEYID123:1678752000:::u:::scESC:")
					return nil
				}
			}
		}
		return nil
	}

	if err := o.GpgSignFile(dummyFile); err != nil {
		t.Fatalf("GpgSignFile failed: %v", err)
	}

	// Verify commands: 1. gpg --show-keys (GpgKeyID), 2. gpg --detach-sign
	if len(cmds) != 2 {
		t.Errorf("Expected 2 commands, got %d", len(cmds))
	}
	if !strings.Contains(cmds[1], "--detach-sign") {
		t.Errorf("Expected detach-sign command, got: %s", cmds[1])
	}
	if !strings.Contains(cmds[1], "KEYID123") {
		t.Errorf("Expected key ID in sign command, got: %s", cmds[1])
	}
}

func TestImportGpgKey(t *testing.T) {
	var lastArgs []string
	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "key.asc")
	if err := os.WriteFile(keyFile, []byte("key data"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.DevGpgHomedir": {filepath.Join(tmpDir, "gpg")},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
		lastArgs = args
		return nil
	}

	if err := o.ImportGpgKey(keyFile); err != nil {
		t.Fatalf("ImportGpgKey failed: %v", err)
	}

	// Expected: gpg --homedir ... --batch --yes --import keyFile
	foundImport := false
	for i, arg := range lastArgs {
		if arg == "--import" && i+1 < len(lastArgs) && lastArgs[i+1] == keyFile {
			foundImport = true
			break
		}
	}
	if !foundImport {
		t.Errorf("ImportGpgKey args missing --import %s: %v", keyFile, lastArgs)
	}
}

func TestGpgKeySelection(t *testing.T) {
	tmpDir := t.TempDir()
	privKey := filepath.Join(tmpDir, "priv.key")
	offKey := filepath.Join(tmpDir, "off.key")

	// Case 1: No keys exist
	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.GpgPublicKey":         {privKey},
			"Ostree.GpgOfficialPublicKey": {offKey},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}
	if _, err := o.AvailableGpgPubKeyPaths(); err == nil {
		t.Error("AvailableGpgPubKeyPaths should fail when no keys exist")
	}

	// Case 2: Only official key exists
	if err := os.WriteFile(offKey, []byte("off"), 0644); err != nil {
		t.Fatal(err)
	}
	paths, err := o.AvailableGpgPubKeyPaths()
	if err != nil {
		t.Errorf("AvailableGpgPubKeyPaths failed: %v", err)
	}
	if len(paths) != 1 || paths[0] != offKey {
		t.Errorf("Expected [offKey], got %v", paths)
	}
	best, _ := o.GpgBestPubKeyPath()
	if best != offKey {
		t.Errorf("Best key should be offKey, got %s", best)
	}

	// Case 3: Both exist (Private should be preferred/first)
	if err := os.WriteFile(privKey, []byte("priv"), 0644); err != nil {
		t.Fatal(err)
	}
	paths, err = o.AvailableGpgPubKeyPaths()
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != privKey {
		t.Errorf("Expected [privKey, offKey], got %v", paths)
	}
	best, _ = o.GpgBestPubKeyPath()
	if best != privKey {
		t.Errorf("Best key should be privKey, got %s", best)
	}
}

func TestPrepareFilesystemHierarchySafety(t *testing.T) {
	imageDir := t.TempDir()
	// Setup initial state
	dirs := []string{"tmp", "etc", "var/db/pkg", "opt", "srv", "home", "usr/local"}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(imageDir, d), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(imageDir, "etc", "machine-id"), []byte("id"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &MockConfig{
		Items: map[string][]string{
			"Releaser.ReadOnlyVdb": {"/usr/var-db-pkg"},
			"Imager.EfiRoot":       {"/efi"},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	// First run
	if err := o.PrepareFilesystemHierarchy(imageDir); err != nil {
		t.Fatalf("First run failed: %v", err)
	}

	// Second run (Safety check)
	err = o.PrepareFilesystemHierarchy(imageDir)
	if err == nil {
		t.Fatal("Second run should have failed due to marker file")
	} else if !strings.Contains(err.Error(), "already prepared") {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestMaybeInitializeGpg(t *testing.T) {
	var cmds [][]string
	tmpDir := t.TempDir()
	privKey := filepath.Join(tmpDir, "priv.key")
	pubKey := filepath.Join(tmpDir, "pub.key")
	offKey := filepath.Join(tmpDir, "off.key")

	for _, f := range []string{privKey, pubKey, offKey} {
		if err := os.WriteFile(f, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.RepoDir":              {"/repo"},
			"Ostree.Remote":               {"origin"},
			"Ostree.GpgPrivateKey":        {privKey},
			"Ostree.GpgPublicKey":         {pubKey},
			"Ostree.GpgOfficialPublicKey": {offKey},
			"Ostree.DevGpgHomedir":        {filepath.Join(tmpDir, "gpg")},
		},
		Bools: map[string]bool{
			"Ostree.Gpg": true,
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
		cmds = append(cmds, args)
		return nil
	}

	if err := o.MaybeInitializeGpg(false); err != nil {
		t.Fatalf("MaybeInitializeGpg failed: %v", err)
	}

	// We expect calls for each key:
	// 1. ImportGpgKey (gpg --import)
	// 2. remote gpg-import (ostree remote gpg-import)
	// Keys: priv, pub, off. (pub is best, off is different)

	// We should see at least 3 ostree remote gpg-import calls and 3 gpg --import calls.
	ostreeImports := 0
	gpgImports := 0

	for _, cmd := range cmds {
		if len(cmd) > 0 {
			if cmd[0] == "--repo=/repo" && cmd[1] == "remote" && cmd[2] == "gpg-import" {
				ostreeImports++
			}
			// Check for gpg --import
			// cmd structure: [gpg --homedir ... --batch --yes --import keyPath]
			for _, arg := range cmd {
				if arg == "--import" {
					gpgImports++
					break
				}
			}
		}
	}

	if ostreeImports != 3 {
		t.Errorf("Expected 3 ostree remote gpg-import calls, got %d", ostreeImports)
	}
	if gpgImports != 3 {
		t.Errorf("Expected 3 gpg --import calls, got %d", gpgImports)
	}
}

func TestPullWithRemoteExplicit(t *testing.T) {
	var lastArgs []string
	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.RepoDir": {"/repo"},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
		lastArgs = args
		return nil
	}

	if err := o.PullWithRemote("myremote", "myref", false); err != nil {
		t.Fatalf("PullWithRemote failed: %v", err)
	}

	// Expected: --repo=/repo pull myremote myref
	if len(lastArgs) < 4 || lastArgs[1] != "pull" || lastArgs[2] != "myremote" || lastArgs[3] != "myref" {
		t.Errorf("PullWithRemote args mismatch: %v", lastArgs)
	}
}

func TestConfigGettersErrors(t *testing.T) {
	cfg := &MockConfig{
		Items: map[string][]string{},
		Bools: map[string]bool{},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	if _, err := o.OsName(); err == nil {
		t.Error("OsName should fail with empty config")
	}
	if _, err := o.Arch(); err == nil {
		t.Error("Arch should fail with empty config")
	}
	if _, err := o.RepoDir(); err == nil {
		t.Error("RepoDir should fail with empty config")
	}
	if _, err := o.Sysroot(); err == nil {
		t.Error("Sysroot should fail with empty config")
	}
	if _, err := o.Remote(); err == nil {
		t.Error("Remote should fail with empty config")
	}
	if _, err := o.RemoteURL(); err == nil {
		t.Error("RemoteURL should fail with empty config")
	}
	if _, err := o.GpgPrivateKeyPath(); err == nil {
		t.Error("GpgPrivateKeyPath should fail with empty config")
	}
	if _, err := o.GpgPublicKeyPath(); err == nil {
		t.Error("GpgPublicKeyPath should fail with empty config")
	}
	if _, err := o.GpgOfficialPubKeyPath(); err == nil {
		t.Error("GpgOfficialPubKeyPath should fail with empty config")
	}
	if _, err := o.FullBranchSuffix(); err == nil {
		t.Error("FullBranchSuffix should fail with empty config")
	}
}

func TestMaybeInitializeRemoteIdempotency(t *testing.T) {
	var cmds []string
	repoDir := t.TempDir()
	// Create objects dir to simulate existing repo
	os.MkdirAll(filepath.Join(repoDir, "objects"), 0755)

	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.RepoDir":   {repoDir},
			"Ostree.Remote":    {"origin"},
			"Ostree.RemoteUrl": {"http://url"},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
		cmds = append(cmds, strings.Join(args, " "))
		// Mock ListRemotes output
		// args: --repo=... remote list
		for i, arg := range args {
			if arg == "remote" && i+1 < len(args) && args[i+1] == "list" {
				fmt.Fprintln(stdout, "origin")
				return nil
			}
		}
		return nil
	}

	if err := o.MaybeInitializeRemote(false); err != nil {
		t.Fatalf("MaybeInitializeRemote failed: %v", err)
	}

	// Should NOT see "init" or "remote add"
	for _, cmd := range cmds {
		if strings.Contains(cmd, "init") {
			t.Error("Should not have initialized repo")
		}
		if strings.Contains(cmd, "remote add") {
			t.Error("Should not have added remote")
		}
	}
}

func setupMinimalHierarchy(t *testing.T, imageDir string) {
	t.Helper()
	dirs := []string{"tmp", "etc", "var/db/pkg", "opt", "srv", "usr/local"}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(imageDir, d), 0755); err != nil {
			t.Fatalf("failed to create dir %s: %v", d, err)
		}
	}
	if err := os.WriteFile(filepath.Join(imageDir, "etc", "machine-id"), []byte("id"), 0644); err != nil {
		t.Fatalf("failed to write machine-id: %v", err)
	}
}

func TestPrepareFilesystemHierarchyEdgeCases(t *testing.T) {
	// Case: Home is a directory
	t.Run("HomeDir", func(t *testing.T) {
		imageDir := t.TempDir()
		setupMinimalHierarchy(t, imageDir)
		os.Mkdir(filepath.Join(imageDir, "home"), 0755)

		cfg := &MockConfig{
			Items: map[string][]string{
				"Releaser.ReadOnlyVdb": {"/usr/var-db-pkg"},
				"Imager.EfiRoot":       {"/efi"},
			},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}
		if err := o.PrepareFilesystemHierarchy(imageDir); err != nil {
			t.Fatalf("PrepareFilesystemHierarchy failed: %v", err)
		}
		// Check if home is now a symlink
		assertSymlink(t, filepath.Join(imageDir, "home"), "var/home")
		// Check if var/home exists
		assertDir(t, filepath.Join(imageDir, "var", "home"))
	})

	// Case: Home is invalid symlink
	t.Run("HomeInvalidSymlink", func(t *testing.T) {
		imageDir := t.TempDir()
		setupMinimalHierarchy(t, imageDir)
		os.MkdirAll(filepath.Join(imageDir, "var", "home"), 0755)
		os.Symlink("/invalid", filepath.Join(imageDir, "home"))

		cfg := &MockConfig{
			Items: map[string][]string{
				"Releaser.ReadOnlyVdb": {"/var/db/pkg"},
				"Imager.EfiRoot":       {"/efi"},
			},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}
		if err := o.PrepareFilesystemHierarchy(imageDir); err == nil {
			t.Error("Expected error for invalid home symlink")
		}
	})
}

func TestListPackagesErrors(t *testing.T) {
	cfg := &MockConfig{
		Items: map[string][]string{}, // Missing ReadOnlyVdb
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}
	if _, err := o.ListPackages("commit", "/sysroot", false); err == nil {
		t.Error("ListPackages should fail if ReadOnlyVdb is missing")
	}

	cfg = &MockConfig{
		Items: map[string][]string{
			"Releaser.ReadOnlyVdb": {"/var/db/pkg"},
		},
	}
	o, _ = NewOstree(cfg)
	// Sysroot does not exist
	if _, err := o.ListPackages("commit", "/sysroot", false); err == nil {
		t.Error("ListPackages should fail if sysroot/var/db/pkg does not exist")
	}
}

func TestPullInvalidRef(t *testing.T) {
	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.RepoDir": {"/repo"},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}
	if err := o.Pull("invalid-ref", false); err == nil {
		t.Error("Pull should fail for ref without remote prefix")
	}
}

func TestGpgArgsEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	pubKey := filepath.Join(tmpDir, "pub.key")
	os.WriteFile(pubKey, []byte("key"), 0644)

	// Mock GpgKeyID execution
	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.DevGpgHomedir": {filepath.Join(tmpDir, "gpg")},
			"Ostree.GpgPublicKey":  {pubKey},
		},
		Bools: map[string]bool{
			"Ostree.Gpg": true,
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
		if len(args) > 0 && args[0] == "--homedir" {
			fmt.Fprintln(stdout, "pub:u:4096:1:KEYID123:1678752000:::u:::scESC:")
		}
		return nil
	}

	args, err := o.GpgArgs()
	if err != nil {
		t.Fatalf("GpgArgs failed: %v", err)
	}
	if len(args) != 2 {
		t.Errorf("Expected 2 args, got %d", len(args))
	}
	if !strings.Contains(args[0], "KEYID123") {
		t.Errorf("Expected key ID in args, got %s", args[0])
	}
}

func TestDeployedRootfsWithSysroot(t *testing.T) {
	origRunCommand := runCommand
	defer func() { runCommand = origRunCommand }()
	runCommand = func(stdout, stderr io.Writer, name string, args ...string) error {
		fmt.Fprintln(stdout, "hash123")
		return nil
	}

	path, err := DeployedRootfsWithSysroot("/sysroot", "/repo", "osname", "ref", false)
	if err != nil {
		t.Fatalf("DeployedRootfsWithSysroot failed: %v", err)
	}
	expected := "/sysroot/ostree/deploy/osname/deploy/hash123.0"
	if path != expected {
		t.Errorf("DeployedRootfsWithSysroot = %q, want %q", path, expected)
	}
}

type errorReader struct{}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, fmt.Errorf("simulated error")
}

func TestReaderHelpers(t *testing.T) {
	// readerToList
	r := strings.NewReader("line1\n  line2  \n\nline3")
	list, err := readerToList(r)
	if err != nil {
		t.Errorf("readerToList failed: %v", err)
	}
	if len(list) != 3 || list[1] != "line2" {
		t.Errorf("readerToList mismatch: %v", list)
	}

	_, err = readerToList(&errorReader{})
	if err == nil {
		t.Error("readerToList should fail with errorReader")
	}

	// readerToFirstNonEmptyLine
	r = strings.NewReader("\n  \n  first  \nsecond")
	line, err := readerToFirstNonEmptyLine(r)
	if err != nil {
		t.Errorf("readerToFirstNonEmptyLine failed: %v", err)
	}
	if line != "first" {
		t.Errorf("readerToFirstNonEmptyLine = %q, want 'first'", line)
	}

	_, err = readerToFirstNonEmptyLine(&errorReader{})
	if err == nil {
		t.Error("readerToFirstNonEmptyLine should fail with errorReader")
	}
}

func TestFileHelpers(t *testing.T) {
	tmpDir := t.TempDir()
	file := filepath.Join(tmpDir, "file")
	os.WriteFile(file, []byte("content"), 0644)

	if !pathExists(file) {
		t.Error("pathExists(file) = false")
	}
	if !pathExists(tmpDir) {
		t.Error("pathExists(dir) = false")
	}
	if pathExists(filepath.Join(tmpDir, "nonexistent")) {
		t.Error("pathExists(nonexistent) = true")
	}

	if !fileExists(file) {
		t.Error("fileExists(file) = false")
	}
	if fileExists(tmpDir) {
		t.Error("fileExists(dir) = true")
	}

	if directoryExists(file) {
		t.Error("directoryExists(file) = true")
	}
	if !directoryExists(tmpDir) {
		t.Error("directoryExists(dir) = false")
	}
}

func TestRunVerbose(t *testing.T) {
	origRunCommand := runCommand
	defer func() { runCommand = origRunCommand }()

	runCommand = func(stdout, stderr io.Writer, name string, args ...string) error {
		if len(args) > 0 && args[0] == "--verbose" {
			return nil
		}
		return fmt.Errorf("expected --verbose")
	}

	if err := Run(true, "arg"); err != nil {
		t.Errorf("Run(true) failed: %v", err)
	}
}

func TestOstreeWrappers(t *testing.T) {
	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.RepoDir": {"/repo"},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
		return nil
	}

	if _, err := o.ListRemotes(false); err != nil {
		t.Error(err)
	}
	if _, err := o.LocalRefs(false); err != nil {
		t.Error(err)
	}
}

func TestListPackagesMocked(t *testing.T) {
	cfg := &MockConfig{
		Items: map[string][]string{
			"Releaser.ReadOnlyVdb": {"/var/db/pkg"},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
		// Mock ls -R output
		output := `d00755 0 0 0 /
d00755 0 0 0 /var/db/pkg/cat/pkg
-00644 0 0 0 /var/db/pkg/cat/pkg/CONTENTS
d00755 0 0 0 /var/db/pkg/cat/other
`
		stdout.Write([]byte(output))
		return nil
	}

	// We need directoryExists to return true for sysroot/var/db/pkg
	sysroot := t.TempDir()
	os.MkdirAll(filepath.Join(sysroot, "var/db/pkg"), 0755)

	pkgs, err := o.ListPackages("commit", sysroot, false)
	if err != nil {
		t.Fatalf("ListPackages failed: %v", err)
	}
	if len(pkgs) != 2 {
		t.Errorf("Expected 2 packages, got %d", len(pkgs))
	}
	if pkgs[0] != "cat/other" || pkgs[1] != "cat/pkg" {
		t.Errorf("Unexpected packages: %v", pkgs)
	}
}

func TestBranchHelpersErrors(t *testing.T) {
	if _, err := BranchShortnameToNormal("", "short", "os", "arch"); err == nil {
		t.Error("Should fail empty stage")
	}
	if _, err := BranchShortnameToNormal("stage", "", "os", "arch"); err == nil {
		t.Error("Should fail empty shortname")
	}
	if _, err := BranchShortnameToNormal("stage", "short", "", "arch"); err == nil {
		t.Error("Should fail empty os")
	}
	if _, err := BranchShortnameToNormal("stage", "short", "os", ""); err == nil {
		t.Error("Should fail empty arch")
	}
}

func TestOstreeBranchMethodsErrors(t *testing.T) {
	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.FullBranchSuffix": {"full"},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	if _, err := o.IsBranchFullSuffixed(""); err == nil {
		t.Error("IsBranchFullSuffixed should fail empty ref")
	}
	if _, err := o.BranchShortnameToFull("", "stage", "os", "arch"); err == nil {
		t.Error("BranchShortnameToFull should fail empty shortname")
	}
	if _, err := o.BranchToFull(""); err == nil {
		t.Error("BranchToFull should fail empty ref")
	}
	if _, err := o.RemoveFullFromBranch(""); err == nil {
		t.Error("RemoveFullFromBranch should fail empty ref")
	}
}

func TestDeploy_Errors(t *testing.T) {
	// Trigger error at specific steps
	tests := []struct {
		name      string
		failAtCmd string
		wantErr   bool
	}{
		{"rev-parse fail", "rev-parse", true},
		{"init-fs fail", "init-fs", true},
		{"os-init fail", "os-init", true},
		{"pull-local fail", "pull-local", true},
		{"refs create fail", "refs", true},
		{"bootloader config fail", "bootloader", true},
		{"deploy fail", "admin deploy", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runCommand = func(stdout, stderr io.Writer, name string, args ...string) error {
				cmdStr := strings.Join(args, " ")
				if strings.Contains(cmdStr, tt.failAtCmd) {
					return fmt.Errorf("simulated error")
				}
				// Mock essential returns
				if len(args) > 0 && args[0] == "rev-parse" {
					stdout.Write([]byte("hash\n"))
				}
				return nil
			}

			cfg := &MockConfig{
				Items: map[string][]string{
					"Ostree.RepoDir":  {"/repo"},
					"Ostree.Sysroot":  {"/sysroot"},
					"Ostree.Remote":   {"origin"},
					"matrixOS.OsName": {"matrixos"},
				},
			}
			o, err := NewOstree(cfg)
			if err != nil {
				t.Fatalf("NewOstree failed: %v", err)
			}

			err = o.Deploy("ref", nil, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("Deploy() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBootedStatus_Errors(t *testing.T) {
	tests := []struct {
		name       string
		jsonOutput string
		mockErr    error
		wantRefErr bool
	}{
		{
			name:       "cmd failed",
			mockErr:    fmt.Errorf("cmd failed"),
			wantRefErr: true,
		},
		{
			name:       "invalid json",
			jsonOutput: "{ invalid json",
			wantRefErr: true,
		},
		{
			name:       "no booted deployment",
			jsonOutput: `{"deployments": [{"booted": false}]}`,
			wantRefErr: true,
		},
	}

	cfg := &MockConfig{Items: map[string][]string{"Ostree.Root": {"/"}}}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runCommand = func(stdout, stderr io.Writer, name string, args ...string) error {
				if tt.mockErr != nil {
					return tt.mockErr
				}
				stdout.Write([]byte(tt.jsonOutput))
				return nil
			}

			_, err := o.BootedRef(false)
			if (err != nil) != tt.wantRefErr {
				t.Errorf("BootedRef() error = %v, wantErr %v", err, tt.wantRefErr)
			}
		})
	}
}

func TestMiscWrappers_Errors(t *testing.T) {
	cfg := &MockConfig{Items: map[string][]string{"Ostree.RepoDir": {"/repo"}}}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
		return fmt.Errorf("cmd error")
	}

	if err := o.Pull("ref", false); err == nil {
		t.Error("Pull should fail on cmd error")
	}
	if err := o.Prune("ref", false); err == nil {
		t.Error("Prune should fail on cmd error")
	}
	if err := o.UpdateSummary(false); err == nil {
		t.Error("UpdateSummary should fail on cmd error")
	}
	if err := o.GenerateStaticDelta("ref", false); err == nil {
		t.Error("GenerateStaticDelta should fail on cmd error")
	}
	if err := o.Upgrade(nil, false); err == nil {
		t.Error("Upgrade should fail on cmd error")
	}
}

func TestLastCommit_Errors(t *testing.T) {
	origRunCommand := runCommand
	defer func() { runCommand = origRunCommand }()

	runCommand = func(stdout, stderr io.Writer, name string, args ...string) error {
		return fmt.Errorf("not found")
	}

	// Test standalone LastCommit if exposed or wrapper
	if _, err := LastCommit("/repo", "ref", false); err == nil {
		t.Error("LastCommit should fail if cmd fails")
	}
}

func TestListRemotes_Errors(t *testing.T) {
	origRunCommand := runCommand
	defer func() { runCommand = origRunCommand }()
	runCommand = func(stdout, stderr io.Writer, name string, args ...string) error {
		return fmt.Errorf("error")
	}

	if _, err := ListRemotes("/repo", false); err == nil {
		t.Error("ListRemotes should fail on error")
	}
}

func TestAddRemote_Error(t *testing.T) {
	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.RepoDir": {"/repo"},
			"Ostree.Remote":  {"origin"},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
		return fmt.Errorf("error")
	}
	if err := o.AddRemote(false); err == nil {
		t.Error("AddRemote should fail on error")
	}
}

func TestValidateFilesystemHierarchy(t *testing.T) {
	tempDir := t.TempDir()

	cfg := &MockConfig{}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	// Sub-test for missing directory
	t.Run("MissingDirectories", func(t *testing.T) {
		err := o.ValidateFilesystemHierarchy(tempDir)
		if err == nil {
			t.Error("expected error for missing directories, got nil")
		}
	})

	// Sub-test for correct hierarchy
	t.Run("ValidHierarchy", func(t *testing.T) {
		// Clean the tempDir for this subtest
		entries, _ := os.ReadDir(tempDir)
		for _, entry := range entries {
			os.RemoveAll(filepath.Join(tempDir, entry.Name()))
		}

		dirs := []string{"/etc", "/home", "/opt", "/root", "/srv", "/tmp", "/usr/local"}
		for _, d := range dirs {
			linkPath := filepath.Join(tempDir, d)
			if d == "/usr/local" {
				os.MkdirAll(filepath.Join(tempDir, "usr"), 0755)
			}

			// Just create some dummy targets
			dummyTarget := filepath.Join(tempDir, "dummy_"+strings.ReplaceAll(d, "/", "_"))
			os.MkdirAll(dummyTarget, 0755)

			if err := os.Symlink(dummyTarget, linkPath); err != nil {
				t.Fatalf("failed to create symlink %s: %v", linkPath, err)
			}
		}

		err := o.ValidateFilesystemHierarchy(tempDir)
		if err != nil {
			t.Errorf("expected nil error for valid hierarchy, got %v", err)
		}
	})

	// Sub-test for regular directory instead of symlink
	t.Run("DirectoryInsteadOfSymlink", func(t *testing.T) {
		// Clean the tempDir for this subtest
		entries, _ := os.ReadDir(tempDir)
		for _, entry := range entries {
			os.RemoveAll(filepath.Join(tempDir, entry.Name()))
		}

		dirs := []string{"/etc", "/home", "/opt", "/root", "/srv", "/tmp", "/usr/local"}
		for _, d := range dirs {
			linkPath := filepath.Join(tempDir, d)
			if d == "/usr/local" {
				os.MkdirAll(filepath.Join(tempDir, "usr"), 0755)
			}
			os.MkdirAll(linkPath, 0755)
		}

		err := o.ValidateFilesystemHierarchy(tempDir)
		if err == nil {
			t.Error("expected error when directories are not symlinks, got nil")
		}
	})
}

func TestListRootRemotes(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		root := t.TempDir()
		cfg := &MockConfig{
			Items: map[string][]string{
				"Ostree.Root": {root},
			},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}

		o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
			stdout.Write([]byte("origin\nbackup\n"))
			return nil
		}

		remotes, err := o.ListRootRemotes(false)
		if err != nil {
			t.Fatalf("ListRootRemotes failed: %v", err)
		}
		if len(remotes) != 2 {
			t.Fatalf("expected 2 remotes, got %d", len(remotes))
		}
		if remotes[0] != "origin" {
			t.Errorf("remotes[0] = %q, want %q", remotes[0], "origin")
		}
		if remotes[1] != "backup" {
			t.Errorf("remotes[1] = %q, want %q", remotes[1], "backup")
		}
	})

	t.Run("VerifiesRepoPath", func(t *testing.T) {
		var capturedArgs []string
		runCommand = func(stdout, stderr io.Writer, name string, args ...string) error {
			capturedArgs = append([]string{name}, args...)
			stdout.Write([]byte("origin\n"))
			return nil
		}

		root := "/my/root"
		cfg := &MockConfig{
			Items: map[string][]string{
				"Ostree.Root": {root},
			},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}

		_, err = o.ListRootRemotes(false)
		if err != nil {
			t.Fatalf("ListRootRemotes failed: %v", err)
		}

		expectedRepoArg := "--repo=/my/root/ostree/repo"
		found := false
		for _, arg := range capturedArgs {
			if arg == expectedRepoArg {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected repo arg %q in command args %v", expectedRepoArg, capturedArgs)
		}
	})

	t.Run("EmptyRoot", func(t *testing.T) {
		cfg := &MockConfig{
			Items: map[string][]string{},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}

		_, err = o.ListRootRemotes(false)
		if err == nil {
			t.Error("expected error for empty root, got nil")
		}
	})

	t.Run("NoRemotes", func(t *testing.T) {
		runCommand = func(stdout, stderr io.Writer, name string, args ...string) error {
			// Empty output
			return nil
		}

		root := t.TempDir()
		cfg := &MockConfig{
			Items: map[string][]string{
				"Ostree.Root": {root},
			},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}

		remotes, err := o.ListRootRemotes(false)
		if err != nil {
			t.Fatalf("ListRootRemotes failed: %v", err)
		}
		if len(remotes) != 0 {
			t.Errorf("expected 0 remotes, got %d", len(remotes))
		}
	})

	t.Run("CommandError", func(t *testing.T) {
		runCommand = func(stdout, stderr io.Writer, name string, args ...string) error {
			return fmt.Errorf("ostree command failed")
		}

		root := t.TempDir()
		cfg := &MockConfig{
			Items: map[string][]string{
				"Ostree.Root": {root},
			},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}

		_, err = o.ListRootRemotes(false)
		if err == nil {
			t.Error("expected error when ostree command fails, got nil")
		}
	})
}

func TestListRootRemoteRefs(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		root := "/myroot"
		cfg := &MockConfig{
			Items: map[string][]string{
				"Ostree.Root":   {root},
				"Ostree.Remote": {"origin"},
			},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}

		o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
			stdout.Write([]byte("matrixos/amd64/gnome\nmatrixos/amd64/server\nmatrixos/amd64/dev/gnome\n"))
			return nil
		}

		refs, err := o.ListRootRemoteRefs(false)
		if err != nil {
			t.Fatalf("ListRootRemoteRefs failed: %v", err)
		}
		if len(refs) != 3 {
			t.Fatalf("expected 3 refs, got %d", len(refs))
		}
		if refs[0] != "matrixos/amd64/gnome" {
			t.Errorf("refs[0] = %q, want %q", refs[0], "matrixos/amd64/gnome")
		}
		if refs[1] != "matrixos/amd64/server" {
			t.Errorf("refs[1] = %q, want %q", refs[1], "matrixos/amd64/server")
		}
		if refs[2] != "matrixos/amd64/dev/gnome" {
			t.Errorf("refs[2] = %q, want %q", refs[2], "matrixos/amd64/dev/gnome")
		}
	})

	t.Run("VerifiesRepoPathAndRemote", func(t *testing.T) {
		var capturedArgs []string
		runCommand = func(stdout, stderr io.Writer, name string, args ...string) error {
			capturedArgs = append([]string{name}, args...)
			stdout.Write([]byte("ref1\n"))
			return nil
		}

		root := "/custom/root"
		cfg := &MockConfig{
			Items: map[string][]string{
				"Ostree.Root":   {root},
				"Ostree.Remote": {"myremote"},
			},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}

		_, err = o.ListRootRemoteRefs(false)
		if err != nil {
			t.Fatalf("ListRootRemoteRefs failed: %v", err)
		}

		expectedRepoArg := "--repo=/custom/root/ostree/repo"
		foundRepo := false
		foundRemote := false
		for _, arg := range capturedArgs {
			if arg == expectedRepoArg {
				foundRepo = true
			}
			if arg == "myremote" {
				foundRemote = true
			}
		}
		if !foundRepo {
			t.Errorf("expected repo arg %q in command args %v", expectedRepoArg, capturedArgs)
		}
		if !foundRemote {
			t.Errorf("expected remote %q in command args %v", "myremote", capturedArgs)
		}
	})

	t.Run("EmptyRoot", func(t *testing.T) {
		cfg := &MockConfig{
			Items: map[string][]string{
				"Ostree.Remote": {"origin"},
			},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}

		_, err = o.ListRootRemoteRefs(false)
		if err == nil {
			t.Error("expected error for empty root, got nil")
		}
	})

	t.Run("EmptyRemote", func(t *testing.T) {
		cfg := &MockConfig{
			Items: map[string][]string{
				"Ostree.Root": {"/some/root"},
			},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}

		_, err = o.ListRootRemoteRefs(false)
		if err == nil {
			t.Error("expected error for empty remote, got nil")
		}
	})

	t.Run("NoRefs", func(t *testing.T) {
		runCommand = func(stdout, stderr io.Writer, name string, args ...string) error {
			return nil
		}

		root := t.TempDir()
		cfg := &MockConfig{
			Items: map[string][]string{
				"Ostree.Root":   {root},
				"Ostree.Remote": {"origin"},
			},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}

		refs, err := o.ListRootRemoteRefs(false)
		if err != nil {
			t.Fatalf("ListRootRemoteRefs failed: %v", err)
		}
		if len(refs) != 0 {
			t.Errorf("expected 0 refs, got %d", len(refs))
		}
	})

	t.Run("CommandError", func(t *testing.T) {
		runCommand = func(stdout, stderr io.Writer, name string, args ...string) error {
			return fmt.Errorf("remote refs failed")
		}

		root := t.TempDir()
		cfg := &MockConfig{
			Items: map[string][]string{
				"Ostree.Root":   {root},
				"Ostree.Remote": {"origin"},
			},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}

		_, err = o.ListRootRemoteRefs(false)
		if err == nil {
			t.Error("expected error when ostree command fails, got nil")
		}
	})
}

func TestListRootDeployments(t *testing.T) {
	fakeJSON := `{
		"deployments": [
			{
				"checksum": "abc123",
				"stateroot": "matrixos",
				"refspec": "origin:matrixos/amd64/gnome",
				"booted": true,
				"pending": false,
				"rollback": false,
				"staged": false,
				"index": 0,
				"serial": 1
			},
			{
				"checksum": "def456",
				"stateroot": "matrixos",
				"refspec": "origin:matrixos/amd64/server",
				"booted": false,
				"pending": false,
				"rollback": true,
				"staged": false,
				"index": 1,
				"serial": 0
			}
		]
	}`

	runCommand = func(stdout, stderr io.Writer, name string, args ...string) error {
		// Expect ostree admin status --json
		stdout.Write([]byte(fakeJSON))
		return nil
	}

	root := t.TempDir()
	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.Root": {root},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	deployments, err := o.ListRootDeployments(false)
	if err != nil {
		t.Fatalf("ListRootDeployments failed: %v", err)
	}

	if len(deployments) != 2 {
		t.Fatalf("expected 2 deployments, got %d", len(deployments))
	}

	// Verify first deployment (booted)
	d0 := deployments[0]
	if d0.Checksum != "abc123" {
		t.Errorf("deployment[0].Checksum = %q, want %q", d0.Checksum, "abc123")
	}
	if d0.Stateroot != "matrixos" {
		t.Errorf("deployment[0].Stateroot = %q, want %q", d0.Stateroot, "matrixos")
	}
	if d0.Refspec != "origin:matrixos/amd64/gnome" {
		t.Errorf("deployment[0].Refspec = %q, want %q", d0.Refspec, "origin:matrixos/amd64/gnome")
	}
	if !d0.Booted {
		t.Error("deployment[0].Booted should be true")
	}
	if d0.Rollback {
		t.Error("deployment[0].Rollback should be false")
	}
	if d0.Index != 0 {
		t.Errorf("deployment[0].Index = %d, want 0", d0.Index)
	}
	if d0.Serial != 1 {
		t.Errorf("deployment[0].Serial = %d, want 1", d0.Serial)
	}

	// Verify second deployment (rollback)
	d1 := deployments[1]
	if d1.Checksum != "def456" {
		t.Errorf("deployment[1].Checksum = %q, want %q", d1.Checksum, "def456")
	}
	if d1.Booted {
		t.Error("deployment[1].Booted should be false")
	}
	if !d1.Rollback {
		t.Error("deployment[1].Rollback should be true")
	}
	if d1.Index != 1 {
		t.Errorf("deployment[1].Index = %d, want 1", d1.Index)
	}
}

func TestListRootDeployments_EmptyRoot(t *testing.T) {
	cfg := &MockConfig{
		Items: map[string][]string{},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	_, err = o.ListRootDeployments(false)
	if err == nil {
		t.Error("expected error for empty root, got nil")
	}
}

func TestListRootDeployments_NoDeployments(t *testing.T) {
	root := t.TempDir()
	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.Root": {root},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
		stdout.Write([]byte(`{"deployments": []}`))
		return nil
	}

	deployments, err := o.ListRootDeployments(false)
	if err != nil {
		t.Fatalf("ListRootDeployments failed: %v", err)
	}
	if len(deployments) != 0 {
		t.Errorf("expected 0 deployments, got %d", len(deployments))
	}
}

func TestListRootDeployments_CommandError(t *testing.T) {
	root := t.TempDir()
	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.Root": {root},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
		return fmt.Errorf("ostree command failed")
	}

	_, err = o.ListRootDeployments(false)
	if err == nil {
		t.Error("expected error when ostree command fails, got nil")
	}
}

func TestListRootDeployments_InvalidJSON(t *testing.T) {
	root := t.TempDir()
	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.Root": {root},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
		stdout.Write([]byte(`{not valid json}`))
		return nil
	}

	_, err = o.ListRootDeployments(false)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestListDeploymentsInChroot(t *testing.T) {
	fakeJSON := `{
		"deployments": [
			{
				"checksum": "chroot111",
				"stateroot": "matrixos",
				"refspec": "origin:matrixos/amd64/dev/gnome",
				"booted": false,
				"pending": false,
				"rollback": false,
				"staged": true,
				"index": 0,
				"serial": 0
			}
		]
	}`

	var capturedArgs []string
	runCommand = func(stdout, stderr io.Writer, name string, args ...string) error {
		capturedArgs = append([]string{name}, args...)
		stdout.Write([]byte(fakeJSON))
		return nil
	}

	root := t.TempDir()
	cfg := &MockConfig{
		Items: map[string][]string{},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	deployments, err := o.ListDeploymentsInChroot(root, false)
	if err != nil {
		t.Fatalf("ListDeploymentsInChroot failed: %v", err)
	}

	if len(deployments) != 1 {
		t.Fatalf("expected 1 deployment, got %d", len(deployments))
	}

	d := deployments[0]
	if d.Checksum != "chroot111" {
		t.Errorf("Checksum = %q, want %q", d.Checksum, "chroot111")
	}
	if d.Refspec != "origin:matrixos/amd64/dev/gnome" {
		t.Errorf("Refspec = %q, want %q", d.Refspec, "origin:matrixos/amd64/dev/gnome")
	}
	if !d.Staged {
		t.Error("Staged should be true")
	}
	if d.Booted {
		t.Error("Booted should be false")
	}

	// Verify the sysroot argument was passed correctly
	expectedSysrootArg := "--sysroot=" + root
	found := false
	for _, arg := range capturedArgs {
		if arg == expectedSysrootArg {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected sysroot arg %q in command args %v", expectedSysrootArg, capturedArgs)
	}
}

func TestListDeploymentsInChroot_EmptyRoot(t *testing.T) {
	cfg := &MockConfig{
		Items: map[string][]string{},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	_, err = o.ListDeploymentsInChroot("", false)
	if err == nil {
		t.Error("expected error for empty root, got nil")
	}
}

func TestListDeploymentsInChroot_CommandError(t *testing.T) {
	root := t.TempDir()
	cfg := &MockConfig{
		Items: map[string][]string{},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
		return fmt.Errorf("chroot ostree failed")
	}

	_, err = o.ListDeploymentsInChroot(root, false)
	if err == nil {
		t.Error("expected error when ostree command fails, got nil")
	}
}

func TestListDeploymentsInChroot_MultipleDeployments(t *testing.T) {
	fakeJSON := `{
		"deployments": [
			{
				"checksum": "aaa111",
				"stateroot": "matrixos",
				"refspec": "origin:matrixos/amd64/gnome",
				"booted": false,
				"pending": true,
				"rollback": false,
				"staged": false,
				"index": 0,
				"serial": 2
			},
			{
				"checksum": "bbb222",
				"stateroot": "matrixos",
				"refspec": "origin:matrixos/amd64/gnome",
				"booted": false,
				"pending": false,
				"rollback": false,
				"staged": false,
				"index": 1,
				"serial": 1
			},
			{
				"checksum": "ccc333",
				"stateroot": "matrixos",
				"refspec": "origin:matrixos/amd64/server",
				"booted": false,
				"pending": false,
				"rollback": true,
				"staged": false,
				"index": 2,
				"serial": 0
			}
		]
	}`

	runCommand = func(stdout, stderr io.Writer, name string, args ...string) error {
		stdout.Write([]byte(fakeJSON))
		return nil
	}

	root := t.TempDir()
	cfg := &MockConfig{
		Items: map[string][]string{},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	deployments, err := o.ListDeploymentsInChroot(root, false)
	if err != nil {
		t.Fatalf("ListDeploymentsInChroot failed: %v", err)
	}

	if len(deployments) != 3 {
		t.Fatalf("expected 3 deployments, got %d", len(deployments))
	}

	// Verify pending deployment
	if !deployments[0].Pending {
		t.Error("deployment[0].Pending should be true")
	}
	if deployments[0].Serial != 2 {
		t.Errorf("deployment[0].Serial = %d, want 2", deployments[0].Serial)
	}

	// Verify rollback deployment
	if !deployments[2].Rollback {
		t.Error("deployment[2].Rollback should be true")
	}
	if deployments[2].Refspec != "origin:matrixos/amd64/server" {
		t.Errorf("deployment[2].Refspec = %q, want %q", deployments[2].Refspec, "origin:matrixos/amd64/server")
	}
}

func TestSwitch(t *testing.T) {
	var lastCmdArgs []string
	sysroot := t.TempDir()
	ref := "origin:matrixos/amd64/gnome"

	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.Sysroot": {sysroot},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
		lastCmdArgs = append([]string{name}, args...)
		return nil
	}

	err = o.Switch(ref, false)
	if err != nil {
		t.Fatalf("Switch failed: %v", err)
	}

	expectedCmd := fmt.Sprintf("ostree admin switch --sysroot=%s %s", sysroot, ref)
	gotCmd := strings.Join(lastCmdArgs, " ")
	if gotCmd != expectedCmd {
		t.Errorf("Command mismatch:\nGot:  %s\nWant: %s", gotCmd, expectedCmd)
	}
}

func TestSwitch_MissingSysroot(t *testing.T) {
	cfg := &MockConfig{
		Items: map[string][]string{},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
		return nil
	}

	err = o.Switch("ref", false)
	if err == nil {
		t.Fatal("Switch should fail when Ostree.Sysroot is missing")
	}
}

func TestSwitch_CommandError(t *testing.T) {
	sysroot := t.TempDir()
	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.Sysroot": {sysroot},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
		return fmt.Errorf("ostree admin switch failed")
	}

	err = o.Switch("ref", false)
	if err == nil {
		t.Fatal("Switch should propagate command error")
	}
}

func TestSwitch_Verbose(t *testing.T) {
	var lastCmdArgs []string
	sysroot := t.TempDir()
	ref := "matrixos/amd64/gnome"

	cfg := &MockConfig{
		Items: map[string][]string{
			"Ostree.Sysroot": {sysroot},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(stdout, stderr io.Writer, name string, args ...string) error {
		lastCmdArgs = append([]string{name}, args...)
		return nil
	}

	err = o.Switch(ref, true)
	if err != nil {
		t.Fatalf("Switch failed: %v", err)
	}

	expectedCmd := fmt.Sprintf("ostree --verbose admin switch --sysroot=%s %s", sysroot, ref)
	gotCmd := strings.Join(lastCmdArgs, " ")
	if gotCmd != expectedCmd {
		t.Errorf("Command mismatch:\nGot:  %s\nWant: %s", gotCmd, expectedCmd)
	}
}
