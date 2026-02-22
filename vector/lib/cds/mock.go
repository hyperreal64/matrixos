package cds

import (
	"strings"

	fslib "matrixos/vector/lib/filesystems"
)

// mockOstree implements IOstree for testing commands.
// Only the fields/methods relevant to each test need to be configured;
// everything else returns safe zero values.
type MockOstree struct {
	Root_            string
	RootErr          error
	Deployments      []Deployment
	DeploymentsErr   error
	Refs             []string
	RefsErr          error
	SwitchRef        string
	SwitchErr        error
	LastCommit_      string
	LastCommitErr    error
	UpgradeArgs      []string
	UpgradeErr       error
	Packages         []string
	PackagesErr      error
	PackagesByCommit map[string][]string

	RemoveFullResult    string
	RemoveFullResultSet bool // when true, return RemoveFullResult even if empty
	RemoveFullErr       error

	BootCommitResult string
	BootCommitErr    error
}

// Config accessors — return zero values (not used in branch/upgrade tests).
func (m *MockOstree) FullBranchSuffix() (string, error)                       { return "-full", nil }
func (m *MockOstree) IsBranchFullSuffixed(string) (bool, error)               { return false, nil }
func (m *MockOstree) BranchShortnameToFull(_, _, _, _ string) (string, error) { return "", nil }
func (m *MockOstree) BranchToFull(string) (string, error)                     { return "", nil }
func (m *MockOstree) RemoveFullFromBranch(ref string) (string, error) {
	if m.RemoveFullErr != nil {
		return "", m.RemoveFullErr
	}
	if m.RemoveFullResultSet {
		return m.RemoveFullResult, nil
	}
	// Default: strip -full suffix if present.
	return strings.TrimSuffix(ref, "-full"), nil
}
func (m *MockOstree) GpgEnabled() (bool, error)                  { return false, nil }
func (m *MockOstree) GpgPrivateKeyPath() (string, error)         { return "", nil }
func (m *MockOstree) GpgPublicKeyPath() (string, error)          { return "", nil }
func (m *MockOstree) GpgOfficialPubKeyPath() (string, error)     { return "", nil }
func (m *MockOstree) OsName() (string, error)                    { return "", nil }
func (m *MockOstree) Arch() (string, error)                      { return "", nil }
func (m *MockOstree) RepoDir() (string, error)                   { return "", nil }
func (m *MockOstree) Sysroot() (string, error)                   { return "", nil }
func (m *MockOstree) Remote() (string, error)                    { return "", nil }
func (m *MockOstree) RemoteURL() (string, error)                 { return "", nil }
func (m *MockOstree) AvailableGpgPubKeyPaths() ([]string, error) { return nil, nil }
func (m *MockOstree) GpgBestPubKeyPath() (string, error)         { return "", nil }
func (m *MockOstree) ClientSideGpgArgs() ([]string, error)       { return nil, nil }
func (m *MockOstree) GpgHomeDir() (string, error)                { return "", nil }
func (m *MockOstree) GpgKeyID() (string, error)                  { return "", nil }
func (m *MockOstree) GpgArgs() ([]string, error)                 { return nil, nil }
func (m *MockOstree) SetupEtc(string) error                      { return nil }
func (m *MockOstree) PrepareFilesystemHierarchy(string) error    { return nil }
func (m *MockOstree) ValidateFilesystemHierarchy(string) error   { return nil }
func (m *MockOstree) BootCommit(string) (string, error) {
	if m.BootCommitErr != nil {
		return "", m.BootCommitErr
	}
	if m.BootCommitResult != "" {
		return m.BootCommitResult, nil
	}
	return "abc123commit", nil
}
func (m *MockOstree) ListRemotes(bool) ([]string, error)                           { return nil, nil }
func (m *MockOstree) ImportGpgKey(string) error                                    { return nil }
func (m *MockOstree) GpgSignFile(string) error                                     { return nil }
func (m *MockOstree) GpgKeys() ([]string, error)                                   { return nil, nil }
func (m *MockOstree) InitializeSigningGpg(bool) error                              { return nil }
func (m *MockOstree) InitializeRemoteSigningGpg(string, string, bool) error        { return nil }
func (m *MockOstree) MaybeInitializeGpg(bool) error                                { return nil }
func (m *MockOstree) MaybeInitializeGpgForRepo(string, string, bool) error         { return nil }
func (m *MockOstree) MaybeInitializeRemote(bool) error                             { return nil }
func (m *MockOstree) Pull(string, bool) error                                      { return nil }
func (m *MockOstree) PullWithRemote(string, string, bool) error                    { return nil }
func (m *MockOstree) Prune(string, bool) error                                     { return nil }
func (m *MockOstree) GenerateStaticDelta(string, bool) error                       { return nil }
func (m *MockOstree) UpdateSummary(bool) error                                     { return nil }
func (m *MockOstree) AddRemote(bool) error                                         { return nil }
func (m *MockOstree) AddRemoteWithSysroot(string, bool) error                      { return nil }
func (m *MockOstree) LocalRefs(bool) ([]string, error)                             { return nil, nil }
func (m *MockOstree) ListContents(string, string, bool) (*[]fslib.PathInfo, error) { return nil, nil }
func (m *MockOstree) ListEtcChanges(string, string) ([]EtcChange, error)           { return nil, nil }
func (m *MockOstree) DeployedRootfs(string, bool) (string, error)                  { return "", nil }
func (m *MockOstree) BootedRef(bool) (string, error)                               { return "", nil }
func (m *MockOstree) BootedHash(bool) (string, error)                              { return "", nil }
func (m *MockOstree) Deploy(string, []string, bool) error                          { return nil }

// Methods with configurable behavior for tests.
func (m *MockOstree) Root() (string, error) {
	if m.Root_ == "" {
		return "/", m.RootErr
	}
	return m.Root_, m.RootErr
}

func (m *MockOstree) ListDeployments(_ bool) ([]Deployment, error) {
	return m.Deployments, m.DeploymentsErr
}

func (m *MockOstree) RemoteRefs(_ bool) ([]string, error) {
	return m.Refs, m.RefsErr
}

func (m *MockOstree) Switch(ref string, _ bool) error {
	m.SwitchRef = ref
	return m.SwitchErr
}

func (m *MockOstree) LastCommit(ref string, _ bool) (string, error) {
	return m.LastCommit_, m.LastCommitErr
}

func (m *MockOstree) Upgrade(args []string, _ bool) error {
	m.UpgradeArgs = args
	return m.UpgradeErr
}

func (m *MockOstree) ListPackages(commit string, _ bool) ([]string, error) {
	if m.PackagesByCommit != nil {
		if pkgs, ok := m.PackagesByCommit[commit]; ok {
			return pkgs, m.PackagesErr
		}
	}
	return m.Packages, m.PackagesErr
}
