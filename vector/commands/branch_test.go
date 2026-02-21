package commands

import (
	"bytes"
	"matrixos/vector/lib/cds"
	fslib "matrixos/vector/lib/filesystems"
	"os"
	"testing"
)

// mockOstree implements cds.IOstree for testing commands.
// Only the fields/methods relevant to each test need to be configured;
// everything else returns safe zero values.
type mockOstree struct {
	root             string
	rootErr          error
	deployments      []cds.Deployment
	deploymentsErr   error
	refs             []string
	refsErr          error
	switchRef        string
	switchErr        error
	lastCommit       string
	lastCommitErr    error
	upgradeErr       error
	upgradeArgs      []string
	packages         []string
	packagesErr      error
	packagesByCommit map[string][]string
}

// Config accessors — return zero values (not used in branch/upgrade tests).
func (m *mockOstree) FullBranchSuffix() (string, error)                            { return "", nil }
func (m *mockOstree) IsBranchFullSuffixed(string) (bool, error)                    { return false, nil }
func (m *mockOstree) BranchShortnameToFull(_, _, _, _ string) (string, error)      { return "", nil }
func (m *mockOstree) BranchToFull(string) (string, error)                          { return "", nil }
func (m *mockOstree) RemoveFullFromBranch(string) (string, error)                  { return "", nil }
func (m *mockOstree) GpgEnabled() (bool, error)                                    { return false, nil }
func (m *mockOstree) GpgPrivateKeyPath() (string, error)                           { return "", nil }
func (m *mockOstree) GpgPublicKeyPath() (string, error)                            { return "", nil }
func (m *mockOstree) GpgOfficialPubKeyPath() (string, error)                       { return "", nil }
func (m *mockOstree) OsName() (string, error)                                      { return "", nil }
func (m *mockOstree) Arch() (string, error)                                        { return "", nil }
func (m *mockOstree) RepoDir() (string, error)                                     { return "", nil }
func (m *mockOstree) Sysroot() (string, error)                                     { return "", nil }
func (m *mockOstree) Remote() (string, error)                                      { return "", nil }
func (m *mockOstree) RemoteURL() (string, error)                                   { return "", nil }
func (m *mockOstree) AvailableGpgPubKeyPaths() ([]string, error)                   { return nil, nil }
func (m *mockOstree) GpgBestPubKeyPath() (string, error)                           { return "", nil }
func (m *mockOstree) ClientSideGpgArgs() ([]string, error)                         { return nil, nil }
func (m *mockOstree) GpgHomeDir() (string, error)                                  { return "", nil }
func (m *mockOstree) GpgKeyID() (string, error)                                    { return "", nil }
func (m *mockOstree) GpgArgs() ([]string, error)                                   { return nil, nil }
func (m *mockOstree) SetupEtc(string) error                                        { return nil }
func (m *mockOstree) PrepareFilesystemHierarchy(string) error                      { return nil }
func (m *mockOstree) ValidateFilesystemHierarchy(string) error                     { return nil }
func (m *mockOstree) BootCommit(string) (string, error)                            { return "", nil }
func (m *mockOstree) ListRemotes(bool) ([]string, error)                           { return nil, nil }
func (m *mockOstree) ListRootRemotes(bool) ([]string, error)                       { return nil, nil }
func (m *mockOstree) LastCommit(string, bool) (string, error)                      { return "", nil }
func (m *mockOstree) LastCommitWithSysroot(string, bool) (string, error)           { return "", nil }
func (m *mockOstree) ImportGpgKey(string) error                                    { return nil }
func (m *mockOstree) GpgSignFile(string) error                                     { return nil }
func (m *mockOstree) MaybeInitializeGpg(bool) error                                { return nil }
func (m *mockOstree) MaybeInitializeGpgForRepo(string, string, bool) error         { return nil }
func (m *mockOstree) MaybeInitializeRemote(bool) error                             { return nil }
func (m *mockOstree) Pull(string, bool) error                                      { return nil }
func (m *mockOstree) PullWithRemote(string, string, bool) error                    { return nil }
func (m *mockOstree) Prune(string, bool) error                                     { return nil }
func (m *mockOstree) GenerateStaticDelta(string, bool) error                       { return nil }
func (m *mockOstree) UpdateSummary(bool) error                                     { return nil }
func (m *mockOstree) AddRemote(bool) error                                         { return nil }
func (m *mockOstree) AddRemoteWithSysroot(string, bool) error                      { return nil }
func (m *mockOstree) LocalRefs(bool) ([]string, error)                             { return nil, nil }
func (m *mockOstree) RemoteRefs(bool) ([]string, error)                            { return nil, nil }
func (m *mockOstree) ListDeploymentsInRoot(string, bool) ([]cds.Deployment, error) { return nil, nil }
func (m *mockOstree) ListContents(string, string, bool) (*[]fslib.PathInfo, error) { return nil, nil }
func (m *mockOstree) DeployedRootfs(string, bool) (string, error)                  { return "", nil }
func (m *mockOstree) BootedRef(bool) (string, error)                               { return "", nil }
func (m *mockOstree) BootedHash(bool) (string, error)                              { return "", nil }
func (m *mockOstree) Deploy(string, []string, bool) error                          { return nil }

// Methods with configurable behavior for tests.
func (m *mockOstree) Root() (string, error) {
	if m.root == "" {
		return "/", m.rootErr
	}
	return m.root, m.rootErr
}

func (m *mockOstree) ListRootDeployments(_ bool) ([]cds.Deployment, error) {
	return m.deployments, m.deploymentsErr
}

func (m *mockOstree) ListRootRemoteRefs(_ bool) ([]string, error) {
	return m.refs, m.refsErr
}

func (m *mockOstree) Switch(ref string, _ bool) error {
	m.switchRef = ref
	return m.switchErr
}

func (m *mockOstree) LastCommitWithRoot(ref string, _ bool) (string, error) {
	return m.lastCommit, m.lastCommitErr
}

func (m *mockOstree) Upgrade(args []string, _ bool) error {
	m.upgradeArgs = args
	return m.upgradeErr
}

func (m *mockOstree) ListPackages(commit string, _ bool) ([]string, error) {
	if m.packagesByCommit != nil {
		if pkgs, ok := m.packagesByCommit[commit]; ok {
			return pkgs, m.packagesErr
		}
	}
	return m.packages, m.packagesErr
}

// newTestBranchCommand creates a BranchCommand with injected mock dependencies,
// bypassing initConfig/initOstree which require real config files.
func newTestBranchCommand(ot cds.IOstree) *BranchCommand {
	cmd := &BranchCommand{}
	cmd.ot = ot
	return cmd
}

// captureStdout runs fn while capturing os.Stdout and returns the output.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

func TestBranchShow(t *testing.T) {
	mock := &mockOstree{
		deployments: []cds.Deployment{
			{
				Booted:    true,
				Checksum:  "abc123",
				Stateroot: "matrixos",
				Refspec:   "origin:matrixos/amd64/gnome",
				Index:     0,
				Serial:    1,
			},
		},
	}
	cmd := newTestBranchCommand(mock)
	if err := cmd.parseArgs([]string{"show"}); err != nil {
		t.Fatalf("parseArgs failed: %v", err)
	}

	output := captureStdout(t, func() {
		if err := cmd.Run(); err != nil {
			t.Fatalf("Run failed: %v", err)
		}
	})

	expected := "Current branch:\n" +
		"  Name: matrixos\n" +
		"  Branch/Ref: origin:matrixos/amd64/gnome\n" +
		"  Checksum: abc123\n" +
		"  Index: 0\n" +
		"  Serial: 1\n"
	if output != expected {
		t.Errorf("output mismatch\nwant: %q\n got: %q", expected, output)
	}
}

func TestBranchShowNoBooted(t *testing.T) {
	mock := &mockOstree{
		deployments: []cds.Deployment{
			{Booted: false, Stateroot: "matrixos"},
		},
	}
	cmd := newTestBranchCommand(mock)
	if err := cmd.parseArgs([]string{"show"}); err != nil {
		t.Fatalf("parseArgs failed: %v", err)
	}

	err := cmd.Run()
	if err == nil {
		t.Fatal("expected error for no booted deployment, got nil")
	}
	if err.Error() != "could not find booted deployment" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBranchList(t *testing.T) {
	mock := &mockOstree{
		refs: []string{"origin:branch1", "origin:branch2"},
	}
	cmd := newTestBranchCommand(mock)
	if err := cmd.parseArgs([]string{"list"}); err != nil {
		t.Fatalf("parseArgs failed: %v", err)
	}

	output := captureStdout(t, func() {
		if err := cmd.Run(); err != nil {
			t.Fatalf("Run failed: %v", err)
		}
	})

	expected := "origin:branch1\norigin:branch2\n"
	if output != expected {
		t.Errorf("output mismatch\nwant: %q\n got: %q", expected, output)
	}
}

func TestBranchSwitch(t *testing.T) {
	mock := &mockOstree{}
	cmd := newTestBranchCommand(mock)
	if err := cmd.parseArgs([]string{"switch", "new/branch"}); err != nil {
		t.Fatalf("parseArgs failed: %v", err)
	}

	if err := cmd.Run(); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if mock.switchRef != "new/branch" {
		t.Errorf("expected switch ref %q, got %q", "new/branch", mock.switchRef)
	}
}

func TestBranchSwitchMissingArg(t *testing.T) {
	mock := &mockOstree{}
	cmd := newTestBranchCommand(mock)
	if err := cmd.parseArgs([]string{"switch"}); err != nil {
		t.Fatalf("parseArgs failed: %v", err)
	}

	err := cmd.Run()
	if err == nil {
		t.Fatal("expected error for missing switch arg, got nil")
	}
}

func TestBranchUnknownSubcommand(t *testing.T) {
	mock := &mockOstree{}
	cmd := newTestBranchCommand(mock)
	if err := cmd.parseArgs([]string{"foo"}); err != nil {
		t.Fatalf("parseArgs failed: %v", err)
	}

	err := cmd.Run()
	if err == nil {
		t.Fatal("expected error for unknown subcommand, got nil")
	}
}

func TestBranchNoSubcommand(t *testing.T) {
	mock := &mockOstree{}
	cmd := newTestBranchCommand(mock)
	err := cmd.parseArgs([]string{})
	if err == nil {
		t.Fatal("expected error for missing subcommand, got nil")
	}
}
