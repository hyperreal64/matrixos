package commands

import (
	"matrixos/vector/lib/cds"
	fslib "matrixos/vector/lib/filesystems"
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
func (m *mockOstree) ListContentsInRoot(string, string, bool) (*[]fslib.PathInfo, error) {
	return nil, nil
}
func (m *mockOstree) ListEtcChanges(string, string) ([]cds.EtcChange, error) { return nil, nil }
func (m *mockOstree) DeployedRootfs(string, bool) (string, error)            { return "", nil }
func (m *mockOstree) BootedRef(bool) (string, error)                         { return "", nil }
func (m *mockOstree) BootedHash(bool) (string, error)                        { return "", nil }
func (m *mockOstree) Deploy(string, []string, bool) error                    { return nil }

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
