package cds

import (
	"fmt"
	"io"
	"matrixos/vector/lib/config"
	fslib "matrixos/vector/lib/filesystems"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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
	cfg := &config.MockConfig{
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

	// Create root first — we need its resolved path to lay out the commit content
	// so that the in-commit paths match what ListPackages looks up via
	// filepath.Join(root, "/var/db/pkg").
	root := t.TempDir()
	// Resolve symlinks (e.g. /tmp -> sysroot/tmp) so ostree can traverse the path.
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}

	// Create content to commit.
	// listPackagesFromPath runs: ostree ls -R commit -- <root>/var/db/pkg
	// so the commit tree must contain the package data at that absolute path.
	contentDir := t.TempDir()
	contentDir, err = filepath.EvalSymlinks(contentDir)
	if err != nil {
		t.Fatal(err)
	}

	// Strip the leading separator so filepath.Join works correctly.
	relRoot := strings.TrimPrefix(root, string(filepath.Separator))
	pkgDir := filepath.Join(contentDir, relRoot, "var", "db", "pkg", "sys-apps", "systemd")
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
	cfg := &config.MockConfig{
		Items: map[string][]string{
			"Releaser.ReadOnlyVdb": {"/var/db/pkg"},
			"Ostree.Root":          {root},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	rootRepo := filepath.Join(root, "ostree", "repo")
	if err := os.MkdirAll(filepath.Dir(rootRepo), 0755); err != nil {
		t.Fatal(err)
	}
	// Symlink the repo we created to the sysroot location
	if err := os.Symlink(repoDir, rootRepo); err != nil {
		t.Fatal(err)
	}
	// Also create the vdb dir in sysroot as ListPackages checks for existence
	if err := os.MkdirAll(filepath.Join(root, "var", "db", "pkg"), 0755); err != nil {
		t.Fatal(err)
	}

	pkgs, err := o.ListPackages(commit, false)
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

	cfg := &config.MockConfig{
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
	cfg := &config.MockConfig{
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

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
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
	cfg := &config.MockConfig{
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
	cfg := &config.MockConfig{
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

	cfg := &config.MockConfig{
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

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
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
	cfg := &config.MockConfig{
		Items: map[string][]string{
			"Ostree.Root": {"/"},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
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

	cfg := &config.MockConfig{
		Items: map[string][]string{
			"Ostree.DevGpgHomedir": {filepath.Join(tmpDir, "gpg")},
			"Ostree.GpgPublicKey":  {pubKey},
		},
	}

	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
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

	cfg := &config.MockConfig{
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
	cfg := &config.MockConfig{
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

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
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

func TestAddRemoteWithSysroot(t *testing.T) {
	var lastArgs []string
	cfg := &config.MockConfig{
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

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
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

	cfg := &config.MockConfig{
		Items: map[string][]string{
			"Ostree.DevGpgHomedir": {filepath.Join(tmpDir, "gpg")},
			"Ostree.GpgPublicKey":  {pubKey},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
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

	cfg := &config.MockConfig{
		Items: map[string][]string{
			"Ostree.DevGpgHomedir": {filepath.Join(tmpDir, "gpg")},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
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
	cfg := &config.MockConfig{
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

	cfg := &config.MockConfig{
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

	cfg := &config.MockConfig{
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

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
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
	cfg := &config.MockConfig{
		Items: map[string][]string{
			"Ostree.RepoDir": {"/repo"},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
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
	cfg := &config.MockConfig{
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

	cfg := &config.MockConfig{
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

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
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

		cfg := &config.MockConfig{
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

		cfg := &config.MockConfig{
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
	cfg := &config.MockConfig{
		Items: map[string][]string{}, // Missing ReadOnlyVdb
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}
	if _, err := o.ListPackages("commit", false); err == nil {
		t.Error("ListPackages should fail if ReadOnlyVdb is missing")
	}

	cfg = &config.MockConfig{
		Items: map[string][]string{
			"Releaser.ReadOnlyVdb": {"/var/db/pkg"},
		},
	}
	o, _ = NewOstree(cfg)
	// Sysroot does not exist
	if _, err := o.ListPackages("commit", false); err == nil {
		t.Error("ListPackages should fail if sysroot/var/db/pkg does not exist")
	}
}

func TestPullInvalidRef(t *testing.T) {
	cfg := &config.MockConfig{
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
	cfg := &config.MockConfig{
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

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
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
	runCommand = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
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

	runCommand = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
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
	cfg := &config.MockConfig{
		Items: map[string][]string{
			"Ostree.RepoDir": {"/repo"},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
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
	cfg := &config.MockConfig{
		Items: map[string][]string{
			"Releaser.ReadOnlyVdb": {"/var/db/pkg"},
			"Ostree.Root":          {"/"},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
		// Mock ls -R output
		output := `d00755 0 0 0 abc abc /
d00755 0 0 0 abc abc /var/db/pkg/cat/pkg
-00644 0 0 0 abc /var/db/pkg/cat/pkg/CONTENTS
d00755 0 0 0 abc abc /var/db/pkg/cat/other
`
		stdout.Write([]byte(output))
		return nil
	}

	// We need directoryExists to return true for sysroot/var/db/pkg
	sysroot := t.TempDir()
	os.MkdirAll(filepath.Join(sysroot, "var/db/pkg"), 0755)

	pkgs, err := o.ListPackages("commit", false)
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
	cfg := &config.MockConfig{
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
			runCommand = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
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

			cfg := &config.MockConfig{
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

	cfg := &config.MockConfig{Items: map[string][]string{"Ostree.Root": {"/"}}}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runCommand = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
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
	cfg := &config.MockConfig{Items: map[string][]string{"Ostree.RepoDir": {"/repo"}}}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
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

	runCommand = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
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
	runCommand = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
		return fmt.Errorf("error")
	}

	if _, err := ListRemotes("/repo", false); err == nil {
		t.Error("ListRemotes should fail on error")
	}
}

func TestAddRemote_Error(t *testing.T) {
	cfg := &config.MockConfig{
		Items: map[string][]string{
			"Ostree.RepoDir": {"/repo"},
			"Ostree.Remote":  {"origin"},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
		return fmt.Errorf("error")
	}
	if err := o.AddRemote(false); err == nil {
		t.Error("AddRemote should fail on error")
	}
}

func TestValidateFilesystemHierarchy(t *testing.T) {
	tempDir := t.TempDir()

	cfg := &config.MockConfig{}
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

func TestRemoteRefs(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		root := "/myroot"
		cfg := &config.MockConfig{
			Items: map[string][]string{
				"Ostree.RepoDir": {filepath.Join(root, "ostree/repo")},
				"Ostree.Remote":  {"origin"},
			},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}

		o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
			stdout.Write([]byte("matrixos/amd64/gnome\nmatrixos/amd64/server\nmatrixos/amd64/dev/gnome\n"))
			return nil
		}

		refs, err := o.RemoteRefs(false)
		if err != nil {
			t.Fatalf("RemoteRefs failed: %v", err)
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
		runCommand = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
			capturedArgs = append([]string{name}, args...)
			stdout.Write([]byte("ref1\n"))
			return nil
		}

		root := "/custom/root"
		cfg := &config.MockConfig{
			Items: map[string][]string{
				"Ostree.RepoDir": {filepath.Join(root, "ostree/repo")},
				"Ostree.Remote":  {"myremote"},
			},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}

		_, err = o.RemoteRefs(false)
		if err != nil {
			t.Fatalf("RemoteRefs failed: %v", err)
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

	t.Run("EmptyRepoDir", func(t *testing.T) {
		cfg := &config.MockConfig{
			Items: map[string][]string{
				"Ostree.Remote": {"origin"},
			},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}

		_, err = o.RemoteRefs(false)
		if err == nil {
			t.Error("expected error for empty repoDir, got nil")
		}
	})

	t.Run("EmptyRemote", func(t *testing.T) {
		root := "/custom/root"
		cfg := &config.MockConfig{
			Items: map[string][]string{
				"Ostree.RepoDir": {filepath.Join(root, "ostree/repo")},
			},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}

		_, err = o.RemoteRefs(false)
		if err == nil {
			t.Error("expected error for empty remote, got nil")
		}
	})

	t.Run("NoRefs", func(t *testing.T) {
		runCommand = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
			return nil
		}

		root := t.TempDir()
		cfg := &config.MockConfig{
			Items: map[string][]string{
				"Ostree.RepoDir": {filepath.Join(root, "ostree/repo")},
				"Ostree.Remote":  {"origin"},
			},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}

		refs, err := o.RemoteRefs(false)
		if err != nil {
			t.Fatalf("RemoteRefs failed: %v", err)
		}
		if len(refs) != 0 {
			t.Errorf("expected 0 refs, got %d", len(refs))
		}
	})

	t.Run("CommandError", func(t *testing.T) {
		runCommand = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
			return fmt.Errorf("remote refs failed")
		}

		root := t.TempDir()
		cfg := &config.MockConfig{
			Items: map[string][]string{
				"Ostree.RepoDir": {filepath.Join(root, "ostree/repo")},
				"Ostree.Remote":  {"origin"},
			},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}

		_, err = o.RemoteRefs(false)
		if err == nil {
			t.Error("expected error when ostree command fails, got nil")
		}
	})
}

func TestListContents(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		cfg := &config.MockConfig{
			Items: map[string][]string{
				"Ostree.RepoDir": {"/repo"},
			},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}

		// Simulate `ostree ls -C -R` output with directories, files, and a symlink.
		mockOutput := `d00755 0 0 0 aaa111 bbb222 /etc
-00644 0 0 42 ccc333 /etc/hostname
l00777 0 0 0 ddd444 /etc/localtime -> /usr/share/zoneinfo/UTC
d00755 0 0 0 eee555 fff666 /etc/conf.d
-00644 0 0 100 ggg777 /etc/conf.d/net
`
		o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
			stdout.Write([]byte(mockOutput))
			return nil
		}

		pis, err := o.ListContents("abc123", "/etc", false)
		if err != nil {
			t.Fatalf("ListContents failed: %v", err)
		}
		if pis == nil {
			t.Fatal("ListContents returned nil")
		}
		if len(*pis) != 5 {
			t.Fatalf("expected 5 entries, got %d", len(*pis))
		}

		// Verify directory entry
		d := (*pis)[0]
		if d.Mode.Type != "d" {
			t.Errorf("entry[0] type = %q, want %q", d.Mode.Type, "d")
		}
		if d.Path != "/etc" {
			t.Errorf("entry[0] path = %q, want %q", d.Path, "/etc")
		}

		// Verify regular file entry
		f := (*pis)[1]
		if f.Mode.Type != "-" {
			t.Errorf("entry[1] type = %q, want %q", f.Mode.Type, "-")
		}
		if f.Path != "/etc/hostname" {
			t.Errorf("entry[1] path = %q, want %q", f.Path, "/etc/hostname")
		}
		if f.Size != 42 {
			t.Errorf("entry[1] size = %d, want 42", f.Size)
		}
		if f.OSTreeChecksum != "ccc333" {
			t.Errorf("entry[1] checksum = %q, want %q", f.OSTreeChecksum, "ccc333")
		}

		// Verify symlink entry
		l := (*pis)[2]
		if l.Mode.Type != "l" {
			t.Errorf("entry[2] type = %q, want %q", l.Mode.Type, "l")
		}
		if l.Path != "/etc/localtime" {
			t.Errorf("entry[2] path = %q, want %q", l.Path, "/etc/localtime")
		}
		if l.Link != "/usr/share/zoneinfo/UTC" {
			t.Errorf("entry[2] link = %q, want %q", l.Link, "/usr/share/zoneinfo/UTC")
		}
	})

	t.Run("EmptyCommit", func(t *testing.T) {
		cfg := &config.MockConfig{
			Items: map[string][]string{
				"Ostree.RepoDir": {"/repo"},
			},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}

		_, err = o.ListContents("", "/etc", false)
		if err == nil {
			t.Error("expected error for empty commit, got nil")
		}
	})

	t.Run("EmptyPath", func(t *testing.T) {
		cfg := &config.MockConfig{
			Items: map[string][]string{
				"Ostree.RepoDir": {"/repo"},
			},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}

		_, err = o.ListContents("abc123", "", false)
		if err == nil {
			t.Error("expected error for empty path, got nil")
		}
	})

	t.Run("MissingRepoDir", func(t *testing.T) {
		cfg := &config.MockConfig{
			Items: map[string][]string{},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}

		_, err = o.ListContents("abc123", "/etc", false)
		if err == nil {
			t.Error("expected error for missing RepoDir, got nil")
		}
	})

	t.Run("CommandError", func(t *testing.T) {
		cfg := &config.MockConfig{
			Items: map[string][]string{
				"Ostree.RepoDir": {"/repo"},
			},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}

		o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
			return fmt.Errorf("ostree ls failed")
		}

		_, err = o.ListContents("abc123", "/etc", false)
		if err == nil {
			t.Error("expected error when command fails, got nil")
		}
	})

	t.Run("EmptyOutput", func(t *testing.T) {
		cfg := &config.MockConfig{
			Items: map[string][]string{
				"Ostree.RepoDir": {"/repo"},
			},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}

		o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
			// Write nothing
			return nil
		}

		pis, err := o.ListContents("abc123", "/etc", false)
		if err != nil {
			t.Fatalf("ListContents failed: %v", err)
		}
		if pis == nil || len(*pis) != 0 {
			t.Errorf("expected empty result, got %v", pis)
		}
	})

	t.Run("VerifiesCommandArgs", func(t *testing.T) {
		var capturedArgs []string
		cfg := &config.MockConfig{
			Items: map[string][]string{
				"Ostree.RepoDir": {"/my/repo"},
			},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}

		o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
			capturedArgs = append([]string{name}, args...)
			return nil
		}

		_, err = o.ListContents("commitABC", "/usr/bin", false)
		if err != nil {
			t.Fatalf("ListContents failed: %v", err)
		}

		// Expected: ostree --repo=/my/repo ls -C -R commitABC -- /usr/bin
		foundRepo := false
		foundLs := false
		foundCommit := false
		foundPath := false
		foundDashDash := false
		for _, arg := range capturedArgs {
			switch arg {
			case "--repo=/my/repo":
				foundRepo = true
			case "ls":
				foundLs = true
			case "commitABC":
				foundCommit = true
			case "/usr/bin":
				foundPath = true
			case "--":
				foundDashDash = true
			}
		}
		if !foundRepo {
			t.Errorf("missing --repo arg in %v", capturedArgs)
		}
		if !foundLs {
			t.Errorf("missing ls arg in %v", capturedArgs)
		}
		if !foundCommit {
			t.Errorf("missing commit arg in %v", capturedArgs)
		}
		if !foundPath {
			t.Errorf("missing path arg in %v", capturedArgs)
		}
		if !foundDashDash {
			t.Errorf("missing -- separator in %v", capturedArgs)
		}
	})

	t.Run("MalformedLine", func(t *testing.T) {
		cfg := &config.MockConfig{
			Items: map[string][]string{
				"Ostree.RepoDir": {"/repo"},
			},
		}
		o, err := NewOstree(cfg)
		if err != nil {
			t.Fatalf("NewOstree failed: %v", err)
		}

		o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
			stdout.Write([]byte("this is not valid ostree ls output\n"))
			return nil
		}

		_, err = o.ListContents("abc123", "/etc", false)
		if err == nil {
			t.Error("expected error for malformed output, got nil")
		}
	})
}

func TestListDeployments(t *testing.T) {
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

	runCommand = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
		// Expect ostree admin status --json
		stdout.Write([]byte(fakeJSON))
		return nil
	}

	root := t.TempDir()
	cfg := &config.MockConfig{
		Items: map[string][]string{
			"Ostree.Root": {root},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	deployments, err := o.ListDeployments(false)
	if err != nil {
		t.Fatalf("ListDeployments failed: %v", err)
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

func TestListDeployments_EmptyRoot(t *testing.T) {
	cfg := &config.MockConfig{
		Items: map[string][]string{},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	_, err = o.ListDeployments(false)
	if err == nil {
		t.Error("expected error for empty root, got nil")
	}
}

func TestListDeployments_NoDeployments(t *testing.T) {
	root := t.TempDir()
	cfg := &config.MockConfig{
		Items: map[string][]string{
			"Ostree.Root": {root},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
		stdout.Write([]byte(`{"deployments": []}`))
		return nil
	}

	deployments, err := o.ListDeployments(false)
	if err != nil {
		t.Fatalf("ListDeployments failed: %v", err)
	}
	if len(deployments) != 0 {
		t.Errorf("expected 0 deployments, got %d", len(deployments))
	}
}

func TestListDeployments_CommandError(t *testing.T) {
	root := t.TempDir()
	cfg := &config.MockConfig{
		Items: map[string][]string{
			"Ostree.Root": {root},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
		return fmt.Errorf("ostree command failed")
	}

	_, err = o.ListDeployments(false)
	if err == nil {
		t.Error("expected error when ostree command fails, got nil")
	}
}

func TestListDeployments_InvalidJSON(t *testing.T) {
	root := t.TempDir()
	cfg := &config.MockConfig{
		Items: map[string][]string{
			"Ostree.Root": {root},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
		stdout.Write([]byte(`{not valid json}`))
		return nil
	}

	_, err = o.ListDeployments(false)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestSwitch(t *testing.T) {
	var lastCmdArgs []string
	sysroot := t.TempDir()
	ref := "origin:matrixos/amd64/gnome"

	cfg := &config.MockConfig{
		Items: map[string][]string{
			"Ostree.Sysroot": {sysroot},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
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
	cfg := &config.MockConfig{
		Items: map[string][]string{},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
		return nil
	}

	err = o.Switch("ref", false)
	if err == nil {
		t.Fatal("Switch should fail when Ostree.Sysroot is missing")
	}
}

func TestSwitch_CommandError(t *testing.T) {
	sysroot := t.TempDir()
	cfg := &config.MockConfig{
		Items: map[string][]string{
			"Ostree.Sysroot": {sysroot},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
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

	cfg := &config.MockConfig{
		Items: map[string][]string{
			"Ostree.Sysroot": {sysroot},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
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

func TestConfigDiff(t *testing.T) {
	root := t.TempDir()

	cfg := &config.MockConfig{
		Items: map[string][]string{
			"Ostree.Root": {root},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	mockOutput := `M    hostname
M    sudoers
M    locale.conf
D    tmpfiles.d/matrixos-live-home.conf
A    NetworkManager/system-connections/Wormhole.nmconnection
A    NetworkManager/system-connections/Insalatina.nmconnection
A    vconsole.conf
A    ostree
`

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
		stdout.Write([]byte(mockOutput))
		return nil
	}

	result, err := o.ConfigDiff(false)
	if err != nil {
		t.Fatalf("ConfigDiff failed: %v", err)
	}

	// Check M entries
	wantM := []string{"hostname", "locale.conf", "sudoers"}
	if gotM, ok := result["M"]; !ok {
		t.Error("expected 'M' key in result")
	} else {
		if len(gotM) != len(wantM) {
			t.Errorf("M entries: got %d, want %d", len(gotM), len(wantM))
		}
		for i, v := range wantM {
			if i >= len(gotM) {
				break
			}
			if gotM[i] != v {
				t.Errorf("M[%d] = %q, want %q", i, gotM[i], v)
			}
		}
	}

	// Check D entries
	wantD := []string{"tmpfiles.d/matrixos-live-home.conf"}
	if gotD, ok := result["D"]; !ok {
		t.Error("expected 'D' key in result")
	} else {
		if len(gotD) != len(wantD) {
			t.Errorf("D entries: got %d, want %d", len(gotD), len(wantD))
		}
		for i, v := range wantD {
			if i >= len(gotD) {
				break
			}
			if gotD[i] != v {
				t.Errorf("D[%d] = %q, want %q", i, gotD[i], v)
			}
		}
	}

	// Check A entries (should be sorted)
	wantA := []string{
		"NetworkManager/system-connections/Insalatina.nmconnection",
		"NetworkManager/system-connections/Wormhole.nmconnection",
		"ostree",
		"vconsole.conf",
	}
	if gotA, ok := result["A"]; !ok {
		t.Error("expected 'A' key in result")
	} else {
		if len(gotA) != len(wantA) {
			t.Errorf("A entries: got %d, want %d", len(gotA), len(wantA))
		}
		for i, v := range wantA {
			if i >= len(gotA) {
				break
			}
			if gotA[i] != v {
				t.Errorf("A[%d] = %q, want %q", i, gotA[i], v)
			}
		}
	}

	// Verify no unexpected keys
	for k := range result {
		if k != "A" && k != "M" && k != "D" {
			t.Errorf("unexpected key %q in result", k)
		}
	}
}

func TestConfigDiff_CommandArgs(t *testing.T) {
	root := t.TempDir()
	var lastCmdArgs []string

	cfg := &config.MockConfig{
		Items: map[string][]string{
			"Ostree.Root": {root},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
		lastCmdArgs = append([]string{name}, args...)
		return nil
	}

	_, err = o.ConfigDiff(false)
	if err != nil {
		t.Fatalf("ConfigDiff failed: %v", err)
	}

	expectedCmd := fmt.Sprintf("ostree admin --sysroot=%s config-diff", root)
	gotCmd := strings.Join(lastCmdArgs, " ")
	if gotCmd != expectedCmd {
		t.Errorf("Command mismatch:\nGot:  %s\nWant: %s", gotCmd, expectedCmd)
	}
}

func TestConfigDiff_Verbose(t *testing.T) {
	root := t.TempDir()
	var lastCmdArgs []string

	cfg := &config.MockConfig{
		Items: map[string][]string{
			"Ostree.Root": {root},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
		lastCmdArgs = append([]string{name}, args...)
		return nil
	}

	_, err = o.ConfigDiff(true)
	if err != nil {
		t.Fatalf("ConfigDiff failed: %v", err)
	}

	// ostreeRunCapture does not pass --verbose to the runner; it only logs to stderr.
	expectedCmd := fmt.Sprintf("ostree admin --sysroot=%s config-diff", root)
	gotCmd := strings.Join(lastCmdArgs, " ")
	if gotCmd != expectedCmd {
		t.Errorf("Command mismatch:\nGot:  %s\nWant: %s", gotCmd, expectedCmd)
	}
}

func TestConfigDiff_EmptyOutput(t *testing.T) {
	root := t.TempDir()

	cfg := &config.MockConfig{
		Items: map[string][]string{
			"Ostree.Root": {root},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
		return nil
	}

	result, err := o.ConfigDiff(false)
	if err != nil {
		t.Fatalf("ConfigDiff failed: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected empty map, got %d keys", len(result))
	}
}

func TestConfigDiff_MissingRoot(t *testing.T) {
	cfg := &config.MockConfig{
		Items: map[string][]string{},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	_, err = o.ConfigDiff(false)
	if err == nil {
		t.Fatal("ConfigDiff should fail when Root is not configured")
	}
}

func TestConfigDiff_CommandError(t *testing.T) {
	root := t.TempDir()

	cfg := &config.MockConfig{
		Items: map[string][]string{
			"Ostree.Root": {root},
		},
	}
	o, err := NewOstree(cfg)
	if err != nil {
		t.Fatalf("NewOstree failed: %v", err)
	}

	o.runner = func(_ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
		return fmt.Errorf("command failed")
	}

	_, err = o.ConfigDiff(false)
	if err == nil {
		t.Fatal("ConfigDiff should propagate command error")
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
