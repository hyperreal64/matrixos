package commands

import (
	"fmt"
	"matrixos/vector/lib/cds"
	"matrixos/vector/lib/config"
)

// IOstree defines the interface for ostree operations used by commands.
// It mirrors all public methods of cds.Ostree for testability.
type IOstree interface {
	// Config accessors
	FullBranchSuffix() (string, error)
	IsBranchFullSuffixed(ref string) (bool, error)
	BranchShortnameToFull(shortName, relStage, osName, arch string) (string, error)
	BranchToFull(ref string) (string, error)
	RemoveFullFromBranch(ref string) (string, error)
	GpgEnabled() (bool, error)
	GpgPrivateKeyPath() (string, error)
	GpgPublicKeyPath() (string, error)
	GpgOfficialPubKeyPath() (string, error)
	OsName() (string, error)
	Arch() (string, error)
	RepoDir() (string, error)
	Sysroot() (string, error)
	Root() (string, error)
	Remote() (string, error)
	RemoteURL() (string, error)
	AvailableGpgPubKeyPaths() ([]string, error)
	GpgBestPubKeyPath() (string, error)
	ClientSideGpgArgs() ([]string, error)
	GpgHomeDir() (string, error)
	GpgKeyID() (string, error)
	GpgArgs() ([]string, error)

	// Filesystem operations
	SetupEtc(imageDir string) error
	PrepareFilesystemHierarchy(imageDir string) error
	ValidateFilesystemHierarchy(imageDir string) error

	// Repo operations
	BootCommit(sysroot string) (string, error)
	ListRemotes(verbose bool) ([]string, error)
	ListRootRemotes(verbose bool) ([]string, error)
	LastCommit(ref string, verbose bool) (string, error)
	LastCommitWithSysroot(ref string, verbose bool) (string, error)
	LastCommitWithRoot(ref string, verbose bool) (string, error)
	ImportGpgKey(keyPath string) error
	GpgSignFile(file string) error
	MaybeInitializeGpg(verbose bool) error
	MaybeInitializeGpgForRepo(remote, repoDir string, verbose bool) error
	MaybeInitializeRemote(verbose bool) error
	Pull(ref string, verbose bool) error
	PullWithRemote(remote, ref string, verbose bool) error
	Prune(ref string, verbose bool) error
	GenerateStaticDelta(ref string, verbose bool) error
	UpdateSummary(verbose bool) error
	AddRemote(verbose bool) error
	AddRemoteWithSysroot(sysroot string, verbose bool) error
	LocalRefs(verbose bool) ([]string, error)
	RemoteRefs(verbose bool) ([]string, error)
	ListRootRemoteRefs(verbose bool) ([]string, error)
	ListRootDeployments(verbose bool) ([]cds.Deployment, error)
	ListDeploymentsInChroot(root string, verbose bool) ([]cds.Deployment, error)
	DeployedRootfs(ref string, verbose bool) (string, error)
	BootedRef(verbose bool) (string, error)
	BootedHash(verbose bool) (string, error)
	Switch(ref string, verbose bool) error
	Deploy(ref string, bootArgs []string, verbose bool) error
	Upgrade(args []string, verbose bool) error
	ListPackages(commit, sysroot string, verbose bool) ([]string, error)
}

type BaseCommand struct {
	cfg config.IConfig
	ot  IOstree
}

func (c *BaseCommand) initConfig() error {
	cfg, err := config.NewIniConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if err := cfg.Load(); err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	c.cfg = cfg
	return nil
}

func (c *BaseCommand) initOstree() error {
	if c.cfg == nil {
		return fmt.Errorf("config not initialized")
	}
	ot, err := cds.NewOstree(c.cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize ostree: %w", err)
	}
	c.ot = ot
	return nil
}
