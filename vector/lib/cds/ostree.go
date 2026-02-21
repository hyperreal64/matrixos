package cds

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"matrixos/vector/lib/config"
	fslib "matrixos/vector/lib/filesystems"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const (
	// EtcActionAdd means the file was added upstream and will appear in /etc.
	EtcActionAdd EtcChangeAction = "add"
	// EtcActionUpdate means upstream modified the file and the user did not;
	// the file in /etc will be replaced with the new version.
	EtcActionUpdate EtcChangeAction = "update"
	// EtcActionRemove means upstream removed the file and the user did not
	// modify it; the file will be removed from /etc.
	EtcActionRemove EtcChangeAction = "remove"
	// EtcActionConflict means both upstream and the user changed the file
	// (or one side added/removed while the other modified); manual
	// resolution is required.
	EtcActionConflict EtcChangeAction = "conflict"
	// EtcActionUserOnly means the user made a change that upstream did not
	// touch; the file in /etc stays as-is.
	EtcActionUserOnly EtcChangeAction = "user-only"
)

// IOstree defines the interface for ostree operations.
// It mirrors all public methods of Ostree for testability.
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
	LastCommit(ref string, verbose bool) (string, error)
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
	ListRootDeployments(verbose bool) ([]Deployment, error)
	ListDeploymentsInRoot(root string, verbose bool) ([]Deployment, error)
	DeployedRootfs(ref string, verbose bool) (string, error)
	BootedRef(verbose bool) (string, error)
	BootedHash(verbose bool) (string, error)
	Switch(ref string, verbose bool) error
	Deploy(ref string, bootArgs []string, verbose bool) error
	Upgrade(args []string, verbose bool) error
	ListPackages(commit string, verbose bool) ([]string, error)
	ListContents(commit, path string, verbose bool) (*[]fslib.PathInfo, error)
	ListContentsInRoot(commit, path string, verbose bool) (*[]fslib.PathInfo, error)
	ListEtcChanges(oldSHA, newSHA string) ([]EtcChange, error)
}

// runCommand runs a generic binary with args and stdout/stderr handling.
var runCommand = func(stdout, stderr io.Writer, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	if err != nil {
		return err
	}
	return nil
}

func readerToList(reader io.Reader) ([]string, error) {
	var elements []string
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		elements = append(elements, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return elements, nil
}

func readerToFirstNonEmptyLine(reader io.Reader) (string, error) {
	scanner := bufio.NewScanner(reader)
	var line string
	for scanner.Scan() {
		line = scanner.Text()
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		break
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return line, nil
}

// SetupEnvironment sets the LC_TIME environment variable to "C".
// This is to ensure that Cloudflare can correctly process requests
// without throwing HTTP 400 errors.
func SetupEnvironment() {
	os.Setenv("LC_TIME", "C")
}

// BranchContainsRemote checks if a branch ref contains a remote.
// A remote is present if the ref contains a ':'.
// The original shell implementation had a bug and was checking for `.*` at the end, not for a colon.
// This implementation follows the function's name intent.
func BranchContainsRemote(branch string) bool {
	return strings.Contains(branch, ":")
}

// ExtractRemoteFromRef extracts the remote name from a ref.
// E.g. "origin:matrixos/dev" -> "origin".
// If no remote is present, returns an empty string.
func ExtractRemoteFromRef(ref string) string {
	parts := strings.SplitN(ref, ":", 2)
	if len(parts) == 2 {
		return parts[0]
	}
	return ""
}

// CleanRemoteFromRef cleans a ref from its remote part.
// E.g. "origin:matrixos/dev" -> "matrixos/dev".
func CleanRemoteFromRef(ref string) string {
	parts := strings.SplitN(ref, ":", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return ref
}

// IsBranchShortName returns true if the branch is a short name.
// E.g. "gnome" -> true, "matrixos/dev/gnome" -> false.
func IsBranchShortName(branch string) bool {
	return !strings.Contains(branch, "/")
}

// BranchShortnameToNormal converts a short branch name to a normal one.
func BranchShortnameToNormal(relStage, shortname, osName, arch string) (string, error) {
	if relStage == "" {
		return "", errors.New("invalid rel stage parameter")
	}
	if shortname == "" {
		return "", errors.New("invalid branch parameter")
	}
	if osName == "" {
		return "", errors.New("invalid os name parameter")
	}
	if arch == "" {
		return "", errors.New("invalid arch parameter")
	}

	nameArch := fmt.Sprintf("%s/%s", osName, arch)
	if relStage == "prod" {
		return fmt.Sprintf("%s/%s", nameArch, shortname), nil
	}
	return fmt.Sprintf("%s/%s/%s", nameArch, relStage, shortname), nil
}

// ClientSideGpgArgs returns arguments for client-side GPG verification.
func ClientSideGpgArgs(gpgEnabled bool, pubKeyPath string) ([]string, error) {
	var gpgArgs []string

	if gpgEnabled {
		gpgArgs = append(
			gpgArgs,
			"--set=gpg-verify=true",
			"--gpg-import="+pubKeyPath,
		)
	} else {
		gpgArgs = append(gpgArgs, "--no-gpg-verify")
	}
	return gpgArgs, nil
}

// ListRemotes lists the remotes in an ostree repository.
func ListRemotes(repoDir string, verbose bool) ([]string, error) {
	if repoDir == "" {
		return nil, errors.New("invalid repoDir parameter")
	}
	stdout, err := RunWithStdoutCapture(
		verbose,
		"--repo="+repoDir,
		"remote",
		"list",
	)
	if err != nil {
		return nil, err
	}
	return readerToList(stdout)
}

// ListLocalRefs lists the local refs in an ostree repo.
func ListLocalRefs(repoDir string, verbose bool) ([]string, error) {
	if repoDir == "" {
		return nil, errors.New("invalid repoDir parameter")
	}
	stdout, err := RunWithStdoutCapture(
		verbose,
		"--repo="+repoDir,
		"refs",
	)
	if err != nil {
		return nil, err
	}
	refs, err := readerToList(stdout)
	if err != nil {
		return nil, err
	}

	refDeleter := func(ref string) bool {
		// Remove ostree-metadata from list.
		if ref == "ostree-metadata" {
			return true
		}
		return false
	}

	return slices.DeleteFunc(refs, refDeleter), nil
}

type AddRemoteOptions struct {
	Remote    string
	RemoteURL string
	GpgArgs   []string
	RepoDir   string
	Sysroot   string
	Verbose   bool
}

func AddRemoteWithOptions(opts AddRemoteOptions, verbose bool) error {
	if opts.Remote == "" {
		return errors.New("invalid Remote parameter")
	}
	if opts.RemoteURL == "" {
		return errors.New("invalid RemoteURL parameter")
	}
	if opts.RepoDir != "" && !directoryExists(opts.RepoDir) {
		return fmt.Errorf("repoDir %s does not exist", opts.RepoDir)
	}
	if opts.Sysroot != "" && !directoryExists(opts.Sysroot) {
		return fmt.Errorf("sysroot %s does not exist", opts.Sysroot)
	}
	args := []string{
		"remote",
		"add",
	}
	if opts.Sysroot != "" {
		args = append(args, "--sysroot="+opts.Sysroot)
	}
	if opts.RepoDir != "" {
		args = append(args, "--repo="+opts.RepoDir)
	}

	args = append(args, "--force")
	args = append(args, opts.GpgArgs...)
	args = append(args, opts.Remote, opts.RemoteURL)
	return Run(verbose, args...)
}

// ListRemoteRefs lists the remote refs present in the given remote.
func ListRemoteRefs(repoDir, remote string, verbose bool) ([]string, error) {
	if repoDir == "" {
		return nil, errors.New("invalid repoDir parameter")
	}
	if remote == "" {
		return nil, errors.New("invalid remote parameter")
	}
	stdout, err := RunWithStdoutCapture(
		verbose,
		"--repo="+repoDir,
		"remote",
		"refs",
		remote,
	)
	if err != nil {
		return nil, err
	}
	return readerToList(stdout)
}

// LastCommit returns the commit hash of the latest commit in the given ref.
func LastCommit(repoDir, ref string, verbose bool) (string, error) {
	if repoDir == "" {
		return "", errors.New("invalid repoDir parameter")
	}
	if ref == "" {
		return "", errors.New("invalid ref parameter")
	}

	stdout, err := RunWithStdoutCapture(
		verbose,
		"rev-parse",
		"--repo="+repoDir,
		ref,
	)
	if err != nil {
		return "", err
	}
	lines, err := readerToList(stdout)
	if err != nil {
		return "", err
	}
	if len(lines) == 0 {
		return "", fmt.Errorf("no commit found for ref %s", ref)
	}
	return lines[0], nil
}

// BuildDeploymentRootfs builds the path to the deployed rootfs given a sysroot, osName,
// commit and index.
func BuildDeploymentRootfs(sysroot, osName, commit string, index int) string {
	return filepath.Join(
		sysroot,
		"ostree",
		"deploy",
		osName,
		"deploy",
		commit+"."+strconv.Itoa(index),
	)
}

// DeployedRootfsWithSysroot returns the path to the deployed rootfs given a sysroot and repoDir.
func DeployedRootfsWithSysroot(sysroot, repoDir, osName, ref string, verbose bool) (string, error) {
	if sysroot == "" {
		return "", errors.New("invalid sysroot parameter")
	}
	if repoDir == "" {
		return "", errors.New("invalid repoDir parameter")
	}
	if osName == "" {
		return "", errors.New("invalid osName parameter")
	}
	if ref == "" {
		return "", errors.New("invalid ref parameter")
	}

	ostreeCommit, err := LastCommit(repoDir, ref, verbose)
	if err != nil {
		return "", fmt.Errorf("cannot get last ostree commit: %w", err)
	}

	rootfs := BuildDeploymentRootfs(sysroot, osName, ostreeCommit, 0)
	return rootfs, nil
}

type Deployment struct {
	Checksum  string `json:"checksum"`
	Stateroot string `json:"stateroot"`
	// Requires matrixOS ostree-2025.7-r1
	Refspec  string `json:"refspec"`
	Booted   bool   `json:"booted"`
	Pending  bool   `json:"pending"`
	Rollback bool   `json:"rollback"`
	Staged   bool   `json:"staged"`
	Index    int    `json:"index"`
	Serial   int    `json:"serial"`
}

func ListDeploymentsWithSysroot(sysroot string, verbose bool) ([]Deployment, error) {
	data, error := ostreeAdminStatusJson(sysroot, verbose)
	if error != nil {
		return nil, error
	}
	if data == nil {
		return nil, errors.New("failed to get ostree status")
	}

	var deployments struct {
		Deployments []Deployment `json:"deployments"`
	}

	if err := json.Unmarshal(*data, &deployments); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ostree status: %w", err)
	}
	return deployments.Deployments, nil
}

func ostreeAdminStatusJson(sysroot string, verbose bool) (*[]byte, error) {
	if sysroot == "" {
		return nil, errors.New("invalid ostree sysroot parameter")
	}
	stdout, err := RunWithStdoutCapture(
		verbose,
		"--sysroot="+sysroot,
		"admin",
		"status",
		"--json",
	)
	if err != nil {
		return nil, err
	}

	data, err := io.ReadAll(stdout)
	if err != nil {
		return nil, fmt.Errorf("failed to read ostree status: %w", err)
	}
	return &data, nil
}

// BootedRefWithSysroot returns the ref of the booted deployment.
func BootedRefWithSysroot(sysroot string, verbose bool) (string, error) {
	if sysroot == "" {
		return "", errors.New("invalid ostree sysroot parameter")
	}

	deployments, err := ListDeploymentsWithSysroot(sysroot, verbose)
	if err != nil {
		return "", err
	}

	for _, d := range deployments {
		if d.Booted {
			return d.Refspec, nil
		}
	}

	return "", errors.New("no booted deployment found")
}

// BootedHash returns the commit hash of the booted deployment.
func BootedHashWithSysroot(sysroot string, verbose bool) (string, error) {
	if sysroot == "" {
		return "", errors.New("invalid ostree sysroot parameter")
	}

	deployments, err := ListDeploymentsWithSysroot(sysroot, verbose)
	if err != nil {
		return "", err
	}

	for _, d := range deployments {
		if d.Booted {
			return d.Checksum, nil
		}
	}

	return "", errors.New("no booted deployment found")
}

// PatchGpgHomeDir sets the correct permissions on the GPG homedir.
func PatchGpgHomeDir(homeDir string) error {
	if homeDir == "" {
		return errors.New("missing homeDir parameter")
	}

	if err := os.MkdirAll(homeDir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(homeDir, 0700); err != nil {
		return err
	}

	err := filepath.Walk(homeDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			if err := os.Chmod(path, 0600); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	curUser, err := user.Current()
	if err != nil {
		return fmt.Errorf("could not find root user: %w", err)
	}
	uid, _ := strconv.Atoi(curUser.Uid)
	gid, _ := strconv.Atoi(curUser.Gid)

	return filepath.Walk(homeDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chown(path, uid, gid)
	})
}

// GpgSignedFilePath returns the path to a GPG signed file.
func GpgSignedFilePath(filePath string) string {
	return filePath + ".asc"
}

// PullWithRemote runs `ostree pull` assuming that the provided ref is
// clean from the remote prefix.
func PullWithRemote(repoDir, remote, ref string, verbose bool) error {
	if repoDir == "" {
		return errors.New("invalid repoDir parameter")
	}
	if remote == "" {
		return errors.New("invalid remote parameter")
	}
	if ref == "" {
		return errors.New("invalid ref parameter")
	}

	fmt.Printf("Pulling ostree from %s %s:%s ...\n", repoDir, remote, ref)
	return Run(verbose, "--repo="+repoDir, "pull", remote, ref)
}

// Pull pulls an ostree ref from a remote using `ostree pull`.
func Pull(repoDir, ref string, verbose bool) error {
	if repoDir == "" {
		return errors.New("invalid repoDir parameter")
	}
	if ref == "" {
		return errors.New("invalid ref parameter")
	}

	remote := ExtractRemoteFromRef(ref)
	if remote == "" {
		return fmt.Errorf("%v does not contain the remote: prefix (e.g. origin:)", ref)
	}
	ref = CleanRemoteFromRef(ref)
	return PullWithRemote(repoDir, remote, ref, verbose)
}

// Prune runs `ostree prune` for the given ref in the given repo.
func Prune(repoDir, ref, keepObjectsYoungerThan string, verbose bool) error {
	if repoDir == "" {
		return errors.New("invalid repoDir parameter")
	}
	if ref == "" {
		return errors.New("invalid ref parameter")
	}
	if keepObjectsYoungerThan == "" {
		return errors.New("invalid keepObjectsYoungerThan parameter")
	}

	fmt.Printf("Pruning ostree repo for %s ...\n", repoDir)
	err := Run(verbose,
		"--repo="+repoDir, "prune",
		"--depth=5",
		"--refs-only",
		"--keep-younger-than="+keepObjectsYoungerThan,
		"--only-branch="+ref,
	)
	return err
}

// CommandRunnerFunc is the function type for executing shell commands.
type CommandRunnerFunc func(stdout, stderr io.Writer, name string, args ...string) error

type Ostree struct {
	cfg    config.IConfig
	runner CommandRunnerFunc
}

// NewOstree creates a new Ostree instance.
func NewOstree(cfg config.IConfig) (*Ostree, error) {
	if cfg == nil {
		return nil, errors.New("missing config parameter")
	}
	return &Ostree{
		cfg:    cfg,
		runner: runCommand,
	}, nil
}

// runCmd runs a command via the instance's command runner, adding --verbose
// and the "ostree" binary name automatically.
func (o *Ostree) runCmd(stdout, stderr io.Writer, verbose bool, args ...string) error {
	var finalArgs []string
	if verbose {
		finalArgs = append(finalArgs, "--verbose")
		fmt.Fprintf(stderr, ">> Executing: ostree --verbose %s\n", strings.Join(args, " "))
	}
	finalArgs = append(finalArgs, args...)
	return o.runner(stdout, stderr, "ostree", finalArgs...)
}

// ostreeRun runs an ostree command with stdout/stderr directed to os.Stdout/os.Stderr.
func (o *Ostree) ostreeRun(verbose bool, args ...string) error {
	return o.runCmd(os.Stdout, os.Stderr, verbose, args...)
}

// ostreeRunCapture runs an ostree command and captures its stdout.
func (o *Ostree) ostreeRunCapture(verbose bool, args ...string) (io.Reader, error) {
	if verbose {
		fmt.Fprintf(os.Stderr, ">> Executing: ostree (stdout capture) %s\n", strings.Join(args, " "))
	}
	stdo := new(bytes.Buffer)
	err := o.runCmd(stdo, os.Stderr, false, args...)
	return stdo, err
}

// listRemotesFromRepo lists remotes using the instance runner.
func (o *Ostree) listRemotesFromRepo(repoDir string, verbose bool) ([]string, error) {
	if repoDir == "" {
		return nil, errors.New("invalid repoDir parameter")
	}
	stdout, err := o.ostreeRunCapture(verbose, "--repo="+repoDir, "remote", "list")
	if err != nil {
		return nil, err
	}
	return readerToList(stdout)
}

// lastCommitFromRepo returns the last commit for a ref using the instance runner.
func (o *Ostree) lastCommitFromRepo(repoDir, ref string, verbose bool) (string, error) {
	if repoDir == "" {
		return "", errors.New("invalid repoDir parameter")
	}
	if ref == "" {
		return "", errors.New("invalid ref parameter")
	}
	stdout, err := o.ostreeRunCapture(verbose, "rev-parse", "--repo="+repoDir, ref)
	if err != nil {
		return "", err
	}
	lines, err := readerToList(stdout)
	if err != nil {
		return "", err
	}
	if len(lines) == 0 {
		return "", fmt.Errorf("no commit found for ref %s", ref)
	}
	return lines[0], nil
}

// listLocalRefsFromRepo lists local refs using the instance runner.
func (o *Ostree) listLocalRefsFromRepo(repoDir string, verbose bool) ([]string, error) {
	if repoDir == "" {
		return nil, errors.New("invalid repoDir parameter")
	}
	stdout, err := o.ostreeRunCapture(verbose, "--repo="+repoDir, "refs")
	if err != nil {
		return nil, err
	}
	return readerToList(stdout)
}

// listRemoteRefsFromRepo lists remote refs using the instance runner.
func (o *Ostree) listRemoteRefsFromRepo(repoDir, remote string, verbose bool) ([]string, error) {
	if repoDir == "" {
		return nil, errors.New("invalid repoDir parameter")
	}
	if remote == "" {
		return nil, errors.New("invalid remote parameter")
	}
	stdout, err := o.ostreeRunCapture(verbose, "--repo="+repoDir, "remote", "refs", remote)
	if err != nil {
		return nil, err
	}
	return readerToList(stdout)
}

// listDeploymentsFromSysroot lists deployments using the instance runner.
func (o *Ostree) listDeploymentsFromSysroot(sysroot string, verbose bool) ([]Deployment, error) {
	if sysroot == "" {
		return nil, errors.New("invalid ostree sysroot parameter")
	}
	stdout, err := o.ostreeRunCapture(verbose, "--sysroot="+sysroot, "admin", "status", "--json")
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(stdout)
	if err != nil {
		return nil, fmt.Errorf("failed to read ostree status: %w", err)
	}
	var deployments struct {
		Deployments []Deployment `json:"deployments"`
	}
	if err := json.Unmarshal(data, &deployments); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ostree status: %w", err)
	}
	return deployments.Deployments, nil
}

// addRemote adds a remote using the instance runner.
func (o *Ostree) addRemote(opts AddRemoteOptions, verbose bool) error {
	if opts.Remote == "" {
		return errors.New("invalid Remote parameter")
	}
	if opts.RemoteURL == "" {
		return errors.New("invalid RemoteURL parameter")
	}
	if opts.RepoDir != "" && !directoryExists(opts.RepoDir) {
		return fmt.Errorf("repoDir %s does not exist", opts.RepoDir)
	}
	if opts.Sysroot != "" && !directoryExists(opts.Sysroot) {
		return fmt.Errorf("sysroot %s does not exist", opts.Sysroot)
	}
	args := []string{"remote", "add"}
	if opts.Sysroot != "" {
		args = append(args, "--sysroot="+opts.Sysroot)
	}
	if opts.RepoDir != "" {
		args = append(args, "--repo="+opts.RepoDir)
	}
	args = append(args, "--force")
	args = append(args, opts.GpgArgs...)
	args = append(args, opts.Remote, opts.RemoteURL)
	return o.ostreeRun(verbose, args...)
}

// pullFromRepo pulls an ostree ref using the instance runner.
func (o *Ostree) pullFromRepo(repoDir, remote, ref string, verbose bool) error {
	if repoDir == "" {
		return errors.New("invalid repoDir parameter")
	}
	if remote == "" {
		return errors.New("invalid remote parameter")
	}
	if ref == "" {
		return errors.New("invalid ref parameter")
	}
	fmt.Printf("Pulling ostree from %s %s:%s ...\n", repoDir, remote, ref)
	return o.ostreeRun(verbose, "--repo="+repoDir, "pull", remote, ref)
}

// pruneFromRepo prunes an ostree repo using the instance runner.
func (o *Ostree) pruneFromRepo(repoDir, ref, keepObjectsYoungerThan string, verbose bool) error {
	if repoDir == "" {
		return errors.New("invalid repoDir parameter")
	}
	if ref == "" {
		return errors.New("invalid ref parameter")
	}
	if keepObjectsYoungerThan == "" {
		return errors.New("invalid keepObjectsYoungerThan parameter")
	}
	fmt.Printf("Pruning ostree repo for %s ...\n", repoDir)
	return o.ostreeRun(verbose,
		"--repo="+repoDir, "prune",
		"--depth=5",
		"--refs-only",
		"--keep-younger-than="+keepObjectsYoungerThan,
		"--only-branch="+ref,
	)
}

func (o *Ostree) FullBranchSuffix() (string, error) {
	suffix, err := o.cfg.GetItem("Ostree.FullBranchSuffix")
	if err != nil {
		return "", err
	}
	if suffix == "" {
		return "", errors.New("missing full branch suffix")
	}
	return suffix, nil
}

// IsBranchFullSuffixed checks if a ref name is a "full" branch.
func (o *Ostree) IsBranchFullSuffixed(ref string) (bool, error) {
	if ref == "" {
		return false, errors.New("missing ref parameter")
	}
	val, err := o.FullBranchSuffix()
	if err != nil {
		return false, err
	}
	return strings.HasSuffix(ref, "-"+val), nil
}

// BranchShortnameToFull converts a short branch name to a full one.
func (o *Ostree) BranchShortnameToFull(shortName, relStage, osName, arch string) (string, error) {
	if shortName == "" {
		return "", errors.New("invalid shortName parameter")
	}
	if relStage == "" {
		return "", errors.New("invalid relStage parameter")
	}
	if osName == "" {
		return "", errors.New("invalid osName parameter")
	}
	if arch == "" {
		return "", errors.New("invalid arch parameter")
	}

	suffixed, err := o.IsBranchFullSuffixed(shortName)
	if err != nil {
		return "", err
	}

	if !suffixed {
		suffix, err := o.FullBranchSuffix()
		if err != nil {
			return "", err
		}
		// Support idempotency.
		shortName = fmt.Sprintf("%s-%s", shortName, suffix)
	}
	return BranchShortnameToNormal(relStage, shortName, osName, arch)
}

// BranchToFull converts a normal branch name to a full one.
func (o *Ostree) BranchToFull(ref string) (string, error) {
	if ref == "" {
		return "", errors.New("invalid ref parameter")
	}

	suffixed, err := o.IsBranchFullSuffixed(ref)
	if err != nil {
		return "", err
	}
	if suffixed {
		// Support idempotency.
		return ref, nil
	}

	suffix, err := o.FullBranchSuffix()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s", ref, suffix), nil
}

// RemoveFullFromBranch removes the "-full" suffix from a branch name.
func (o *Ostree) RemoveFullFromBranch(ref string) (string, error) {
	if ref == "" {
		return "", errors.New("invalid ref parameter")
	}

	suffixed, err := o.IsBranchFullSuffixed(ref)
	if err != nil {
		return "", err
	}
	if !suffixed {
		return ref, nil
	}

	suffix, err := o.FullBranchSuffix()
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(ref, "-"+suffix), nil
}

func run(stdout, stderr io.Writer, verbose bool, args ...string) error {
	var finalArgs []string
	if verbose {
		finalArgs = append(finalArgs, "--verbose")
		fmt.Fprintf(stderr, ">> Executing: ostree --verbose %s\n", strings.Join(args, " "))
	}
	finalArgs = append(finalArgs, args...)
	return runCommand(stdout, stderr, "ostree", finalArgs...)
}

// Run runs an ostree command with --verbose if requested.
var Run = func(verbose bool, args ...string) error {
	return run(os.Stdout, os.Stderr, verbose, args...)
}

// RunWithStdoutCapture runs an ostree command and captures its stdout,
// with --verbose if requested.
var RunWithStdoutCapture = func(verbose bool, args ...string) (io.Reader, error) {
	if verbose {
		fmt.Fprintf(os.Stderr, ">> Executing: ostree (stdout capture) %s\n", strings.Join(args, " "))
	}
	stdo := new(bytes.Buffer)
	err := run(stdo, os.Stderr, false /* do not run ostree with verbose! */, args...)
	return stdo, err
}

// CollectionIDArgs returns the ostree --collection-id argument if a collection ID is provided.
func CollectionIDArgs(collectionID string) ([]string, error) {
	if collectionID == "" {
		return nil, errors.New("missing collectionID parameter")
	}

	var args []string
	if collectionID != "" {
		args = append(args, "--collection-id="+collectionID)
	}
	return args, nil
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func directoryExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// GpgEnabled returns whether GPG signing and verification is enabled.
func (o *Ostree) GpgEnabled() (bool, error) {
	return o.cfg.GetBool("Ostree.Gpg")
}

// GpgPublicKeyPath returns the user defined private/ placed
// GPG private key path.
func (o *Ostree) GpgPrivateKeyPath() (string, error) {
	pk, err := o.cfg.GetItem("Ostree.GpgPrivateKey")
	if err != nil {
		return "", err
	}
	if pk == "" {
		return "", errors.New("invalid Ostree.GpgPrivateKey")
	}
	return pk, nil
}

// GpgPublicKeyPath returns the user defined private/ placed
// GPG public key path.
func (o *Ostree) GpgPublicKeyPath() (string, error) {
	pk, err := o.cfg.GetItem("Ostree.GpgPublicKey")
	if err != nil {
		return "", err
	}
	if pk == "" {
		return "", errors.New("invalid Ostree.GpgPublicKey")
	}
	return pk, nil
}

// GpgOfficialPubKeyPath returns the official, git repository distributed
// GPG public key path.
func (o *Ostree) GpgOfficialPubKeyPath() (string, error) {
	pk, err := o.cfg.GetItem("Ostree.GpgOfficialPublicKey")
	if err != nil {
		return "", err
	}
	if pk == "" {
		return "", errors.New("invalid Ostree.GpgOfficialPublicKey")
	}
	return pk, nil
}

// OsName returns the name of the OS as defined in the config.
func (o *Ostree) OsName() (string, error) {
	name, err := o.cfg.GetItem("matrixOS.OsName")
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", errors.New("invalid matrixOS.OsName")
	}
	return name, nil
}

// Arch returns the build architecture as defined in the config.
func (o *Ostree) Arch() (string, error) {
	arch, err := o.cfg.GetItem("matrixOS.Arch")
	if err != nil {
		return "", err
	}
	if arch == "" {
		return "", errors.New("invalid matrixOS.Arch")
	}
	return arch, nil
}

// RepoDir returns the path to the ostree repository.
func (o *Ostree) RepoDir() (string, error) {
	repoDir, err := o.cfg.GetItem("Ostree.RepoDir")
	if err != nil {
		return "", err
	}
	if repoDir == "" {
		return "", errors.New("invalid Ostree.RepoDir")
	}
	return repoDir, nil
}

// Sysroot returns the path to the ostree sysroot directory. Usually /sysroot.
func (o *Ostree) Sysroot() (string, error) {
	sysroot, err := o.cfg.GetItem("Ostree.Sysroot")
	if err != nil {
		return "", err
	}
	if sysroot == "" {
		return "", errors.New("invalid Ostree.Sysroot")
	}
	return sysroot, nil
}

// Root returns the path to the root filesystem directory used as root for
// ostree operations (i.e. --sysroot).
func (o *Ostree) Root() (string, error) {
	root, err := o.cfg.GetItem("Ostree.Root")
	if err != nil {
		return "", err
	}
	if root == "" {
		return "", errors.New("invalid Ostree.Root")
	}
	return root, nil
}

// Remote returns the name of the remote.
func (o *Ostree) Remote() (string, error) {
	remote, err := o.cfg.GetItem("Ostree.Remote")
	if err != nil {
		return "", err
	}
	if remote == "" {
		return "", errors.New("invalid Ostree.Remote")
	}
	return remote, nil
}

// RemoteURL returns the URL of the remote.
func (o *Ostree) RemoteURL() (string, error) {
	url, err := o.cfg.GetItem("Ostree.RemoteUrl")
	if err != nil {
		return "", err
	}
	if url == "" {
		return "", errors.New("invalid Ostree.RemoteUrl")
	}
	return url, nil
}

// AvailableGpgPubKeyPaths returns the list of available (file exists)
// GPG public key paths.
func (o *Ostree) AvailableGpgPubKeyPaths() ([]string, error) {
	var candidates []string
	privatePubKeyPath, err := o.GpgPublicKeyPath()
	if err == nil {
		candidates = append(candidates, privatePubKeyPath)
	}
	officialPubKeyPath, err := o.GpgOfficialPubKeyPath()
	if err == nil {
		candidates = append(candidates, officialPubKeyPath)
	}

	var paths []string
	for _, path := range candidates {
		if fileExists(path) {
			paths = append(paths, path)
		}
	}
	if len(paths) == 0 {
		return paths, fmt.Errorf(
			"unable to find a valid GPG pub key. Neither: %v nor %v exist",
			privatePubKeyPath,
			officialPubKeyPath,
		)
	}

	return paths, nil
}

// GpgBestPubKeyPath returns the path to the GPG public key to use.
// It prefers the private key path over the official one.
func (o *Ostree) GpgBestPubKeyPath() (string, error) {
	paths, err := o.AvailableGpgPubKeyPaths()
	if err != nil {
		return "", err
	}
	// pick the first, it's the best always.
	return paths[0], nil
}

// ClientSideGpgArgs returns arguments for client-side GPG verification.
func (o *Ostree) ClientSideGpgArgs() ([]string, error) {
	gpgEnabled, err := o.GpgEnabled()
	if err != nil {
		return nil, err
	}
	var pubKeyPath string
	if gpgEnabled {
		pubKeyPath, err = o.GpgBestPubKeyPath()
		if err != nil {
			return nil, err
		}
	}
	return ClientSideGpgArgs(gpgEnabled, pubKeyPath)
}

// SetupEtc moves the /etc directory to /usr/etc.
func (o *Ostree) SetupEtc(imageDir string) error {
	fmt.Println("Setting up /etc...")
	etcDir := filepath.Join(imageDir, "etc")
	usrEtcDir := filepath.Join(imageDir, "usr", "etc")

	fmt.Printf("Moving %s to %s\n", etcDir, usrEtcDir)
	return os.Rename(etcDir, usrEtcDir)
}

// BootCommit returns the boot commit from an ostree sysroot.
func (o *Ostree) BootCommit(sysroot string) (string, error) {
	osName, err := o.OsName()
	if err != nil {
		return "", err
	}
	bootPrefix := filepath.Join(sysroot, "ostree", "boot.1", osName)
	files, err := os.ReadDir(bootPrefix)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", fmt.Errorf("no commit found in %s", bootPrefix)
	}
	return files[0].Name(), nil
}

// ListRemotes lists all the remote refs in the configuration's ostree repository.
func (o *Ostree) ListRemotes(verbose bool) ([]string, error) {
	repoDir, err := o.RepoDir()
	if err != nil {
		return nil, err
	}
	return o.listRemotesFromRepo(repoDir, verbose)
}

// LastCommit returns the last commit for a given ref.
func (o *Ostree) LastCommit(ref string, verbose bool) (string, error) {
	repoDir, err := o.RepoDir()
	if err != nil {
		return "", err
	}
	return o.lastCommitFromRepo(repoDir, ref, verbose)
}

// LastCommitWithRoot returns the last commit for a given ref in the root filesystem.
func (o *Ostree) LastCommitWithRoot(ref string, verbose bool) (string, error) {
	root, err := o.Root()
	if err != nil {
		return "", err
	}
	repoDir := filepath.Join(root, "ostree", "repo")
	return o.lastCommitFromRepo(repoDir, ref, verbose)
}

func (o *Ostree) getDevGpgHomedir() (string, error) {
	dir, err := o.cfg.GetItem("Ostree.DevGpgHomedir")
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", errors.New("invalid Ostree.DevGpgHomedir")
	}
	return dir, nil
}

// GpgHomeDir returns the path to the GPG homedir, creating and setting permissions if needed.
func (o *Ostree) GpgHomeDir() (string, error) {
	devGpgHomeDir, err := o.getDevGpgHomedir()
	if err != nil {
		return "", err
	}
	if err := PatchGpgHomeDir(devGpgHomeDir); err != nil {
		return "", err
	}
	return devGpgHomeDir, nil
}

// GpgKeyID returns the GPG key ID to use for signing.
func (o *Ostree) GpgKeyID() (string, error) {
	homeDir, err := o.GpgHomeDir()
	if err != nil {
		return "", err
	}
	pubkeyPath, err := o.GpgBestPubKeyPath()
	if err != nil {
		return "", err
	}

	out := new(bytes.Buffer)
	err = o.runner(
		out,
		os.Stderr,
		"gpg",
		"--homedir", homeDir,
		"--batch",
		"--yes",
		"--with-colons",
		"--show-keys",
		"--keyid-format", "LONG",
		pubkeyPath,
	)
	if err != nil {
		return "", err
	}

	var keyID string
	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "pub") {
			continue
		}

		parts := strings.Split(line, ":")
		if len(parts) >= 5 {
			keyID = strings.TrimSpace(parts[4])
			break
		}
	}

	err = scanner.Err()
	if err != nil {
		return "", err
	}

	if keyID == "" {
		return keyID, errors.New("cannot find gpg ostree key id.")
	}
	return keyID, nil
}

// ImportGpgKey imports a GPG key into the GPG homedir.
func (o *Ostree) ImportGpgKey(keyPath string) error {
	if keyPath == "" {
		return errors.New("missing keyPath parameter")
	}
	if !fileExists(keyPath) {
		return fmt.Errorf("file %s does not exist", keyPath)
	}

	homeDir, err := o.GpgHomeDir()
	if err != nil {
		return err
	}

	return o.runner(
		os.Stdout,
		os.Stderr,
		"gpg",
		"--homedir", homeDir,
		"--batch", "--yes",
		"--import", keyPath,
	)
}

// GpgSignFile signs a file with GPG.
func (o *Ostree) GpgSignFile(file string) error {
	if file == "" {
		return errors.New("missing file parameter")
	}
	if !fileExists(file) {
		return fmt.Errorf("file %s does not exist", file)
	}

	homeDir, err := o.GpgHomeDir()
	if err != nil {
		return err
	}

	keyID, err := o.GpgKeyID()
	if err != nil {
		return err
	}

	ascFile := GpgSignedFilePath(file)

	err = o.runner(
		os.Stdout,
		os.Stderr,
		"gpg",
		"--homedir", homeDir,
		"--batch", "--yes",
		"--local-user", keyID,
		"--armor",
		"--detach-sign",
		"--output", ascFile,
		file,
	)
	if err != nil {
		return err
	}

	fmt.Printf("GPG signature file %v created.\n", ascFile)
	return nil
}

func (o *Ostree) initializeGpg(remote, repoDir string, verbose bool) error {
	if remote == "" {
		return errors.New("missing remote parameter")
	}
	if repoDir == "" {
		return errors.New("missing repoDir parameter")
	}

	var keys []string

	gpgKeyPath, err := o.GpgPrivateKeyPath()
	if err != nil {
		return err
	}
	keys = append(keys, gpgKeyPath)

	signingPubKey, err := o.GpgBestPubKeyPath()
	if err != nil {
		return err
	}
	keys = append(keys, signingPubKey)

	officialPubKeyPath, err := o.GpgOfficialPubKeyPath()
	if err != nil {
		return err
	}
	// if it's the same as signingPubKey, do not add a dup.
	if signingPubKey != officialPubKeyPath {
		keys = append(keys, officialPubKeyPath)
	}

	for _, key := range keys {
		if !fileExists(key) {
			fmt.Fprintf(os.Stderr, "WARNING: %s not present, skipping import ...\n", key)
			continue
		}
		if err := o.ImportGpgKey(key); err != nil {
			return fmt.Errorf("failed to import gpg key %s: %w", key, err)
		}

		err := o.ostreeRun(verbose, "--repo="+repoDir, "remote", "gpg-import", remote, "-k", key)
		if err != nil {
			return fmt.Errorf("failed to import gpg key %s to remote %s: %w", key, remote, err)
		}
	}
	return nil
}

// MaybeInitializeGpg initializes GPG keys for an ostree repository.
func (o *Ostree) MaybeInitializeGpg(verbose bool) error {
	repoDir, err := o.RepoDir()
	if err != nil {
		return err
	}
	remote, err := o.Remote()
	if err != nil {
		return err
	}

	return o.MaybeInitializeGpgForRepo(remote, repoDir, verbose)
}

// MaybeInitializeGpg initializes GPG keys for an ostree repository.
func (o *Ostree) MaybeInitializeGpgForRepo(remote, repoDir string, verbose bool) error {
	gpgEnabled, err := o.GpgEnabled()
	if err != nil {
		return err
	}
	if !gpgEnabled {
		fmt.Println("GPG signing is disabled. Skipping GPG initialization ...")
		return nil
	}

	return o.initializeGpg(remote, repoDir, verbose)
}

// MaybeInitializeRemote initializes an ostree remote.
func (o *Ostree) MaybeInitializeRemote(verbose bool) error {
	repoDir, err := o.RepoDir()
	if err != nil {
		return err
	}
	if !directoryExists(repoDir) {
		if err := os.MkdirAll(repoDir, 0755); err != nil {
			return err
		}
	}

	remote, err := o.Remote()
	if err != nil {
		return err
	}
	remoteURL, err := o.RemoteURL()
	if err != nil {
		return err
	}

	objectsDir := filepath.Join(repoDir, "objects")
	if !directoryExists(objectsDir) {
		fmt.Printf("Initializing local ostree repo at %v ...\n", repoDir)
		err := o.ostreeRun(verbose, "--repo="+repoDir, "init", "--mode=archive")
		if err != nil {
			return err
		}
	} else {
		fmt.Printf("ostree repo at %v already initialized. Reusing ...\n", repoDir)
	}

	remotes, err := o.listRemotesFromRepo(repoDir, verbose)
	if err != nil {
		return err
	}
	remoteFound := slices.Contains(remotes, remote)
	if remoteFound {
		fmt.Printf("Remote %v already exists, reusing ...\n", remote)
	} else {
		fmt.Printf("Initializing remote %v at %v ...\n", remote, repoDir)
		gpgArgs, err := o.ClientSideGpgArgs()
		if err != nil {
			return err
		}
		args := []string{"--repo=" + repoDir, "remote", "add"}
		args = append(args, gpgArgs...)
		args = append(args, remote, remoteURL)
		err = o.ostreeRun(verbose, args...)
		if err != nil {
			return err
		}
	}

	fmt.Println("Showing current ostree remotes:")
	err = o.ostreeRun(verbose, "--repo="+repoDir, "remote", "list", "-u")
	return err
}

// Pull pulls an ostree ref from a remote.
func (o *Ostree) Pull(ref string, verbose bool) error {
	if ref == "" {
		return errors.New("invalid ref parameter")
	}
	repoDir, err := o.RepoDir()
	if err != nil {
		return err
	}
	remote := ExtractRemoteFromRef(ref)
	if remote == "" {
		return fmt.Errorf("%v does not contain the remote: prefix (e.g. origin:)", ref)
	}
	ref = CleanRemoteFromRef(ref)
	return o.pullFromRepo(repoDir, remote, ref, verbose)
}

// PullWithRemote runs `ostree pull` assuming that the provided ref is
// clean from the remote prefix.
func (o *Ostree) PullWithRemote(remote, ref string, verbose bool) error {
	if remote == "" {
		return errors.New("invalid remote parameter")
	}
	if ref == "" {
		return errors.New("invalid ref parameter")
	}
	repoDir, err := o.RepoDir()
	if err != nil {
		return err
	}
	return o.pullFromRepo(repoDir, remote, ref, verbose)
}

// GpgArgs returns the gpg arguments for ostree commands.
func (o *Ostree) GpgArgs() ([]string, error) {
	gpgEnabled, err := o.GpgEnabled()
	if err != nil {
		return nil, err
	}
	if !gpgEnabled {
		return nil, nil
	}

	keyID, err := o.GpgKeyID()
	if err != nil {
		return nil, err
	}

	homeDir, err := o.GpgHomeDir()
	if err != nil {
		return nil, err
	}

	return []string{
		"--gpg-sign=" + keyID,
		"--gpg-homedir=" + homeDir,
	}, nil
}

// Prune prunes the ostree repo for the given ref.
func (o *Ostree) Prune(ref string, verbose bool) error {
	if ref == "" {
		return errors.New("invalid ref parameter")
	}
	repoDir, err := o.RepoDir()
	if err != nil {
		return err
	}
	keepObjectsYoungerThan, err := o.cfg.GetItem("Ostree.KeepObjectsYoungerThan")
	if err != nil {
		return err
	}
	return o.pruneFromRepo(repoDir, ref, keepObjectsYoungerThan, verbose)
}

// GenerateStaticDelta generates a static delta for an ostree repository.
func (o *Ostree) GenerateStaticDelta(ref string, verbose bool) error {
	if ref == "" {
		return errors.New("invalid ref parameter")
	}

	repoDir, err := o.RepoDir()
	if err != nil {
		return err
	}

	fmt.Printf("Generating static delta for %s and ref %s ...\n", repoDir, ref)

	stdout, err := o.ostreeRunCapture(
		verbose,
		"--repo="+repoDir,
		"rev-parse",
		ref,
	)
	if err != nil {
		return err
	}

	revNew, err := readerToFirstNonEmptyLine(stdout)
	if err != nil {
		return err
	}

	stdout, err = o.ostreeRunCapture(
		verbose,
		"--repo="+repoDir,
		"rev-parse",
		ref+"^",
	)
	if err != nil {
		// This is not a fatal error, the branch might not have a previous commit.
	}
	revOld, _ := readerToFirstNonEmptyLine(stdout)

	if revOld != "" {
		err := o.runCmd(
			io.Discard,
			os.Stderr,
			verbose,
			"--repo="+repoDir,
			"rev-parse",
			revOld,
		)
		if err != nil {
			fmt.Fprintf(
				os.Stderr,
				"WARNING: rev-parse for old revision %s failed, Falling back to full delta ...\n",
				revOld,
			)
			revOld = ""
		}
	}
	// SAFETY CHECK: Does the parent object actually exist?
	if revOld != "" {
		err := o.runCmd(
			io.Discard,
			os.Stderr,
			verbose,
			"show",
			"--repo="+repoDir,
			revOld,
		)
		if err != nil {
			fmt.Fprintf(
				os.Stderr,
				"WARNING: Parent commit %s is referenced but missing. Falling back to full delta.\n",
				revOld,
			)
			revOld = ""
		}
	}

	args := []string{
		"--repo=" + repoDir,
		"static-delta", "generate",
		"--to=" + revNew,
		"--inline",
		"--min-fallback-size=0",
		"--disable-bsdiff",
		"--max-chunk-size=64",
	}

	if revOld == "" {
		args = append(args, "--empty")
	} else {
		args = append(args, "--from="+revOld)
	}

	return o.ostreeRun(verbose, args...)
}

// UpdateSummary updates the summary of an ostree repository.
func (o *Ostree) UpdateSummary(verbose bool) error {
	fmt.Println("Updating ostree summary ...")

	repoDir, err := o.RepoDir()
	if err != nil {
		return err
	}

	args := []string{
		"--repo=" + repoDir,
		"summary",
		"--update",
	}

	gpgArgs, err := o.GpgArgs()
	if err != nil {
		return err
	}
	args = append(args, gpgArgs...)

	return o.ostreeRun(verbose, args...)
}

// AddRemote adds a remote to an ostree repo.
func (o *Ostree) AddRemote(verbose bool) error {
	repoDir, err := o.RepoDir()
	if err != nil {
		return err
	}

	gpgArgs, err := o.ClientSideGpgArgs()
	if err != nil {
		return err
	}

	remote, err := o.Remote()
	if err != nil {
		return err
	}
	remoteURL, err := o.RemoteURL()
	if err != nil {
		return err
	}

	opts := AddRemoteOptions{
		Remote:    remote,
		RemoteURL: remoteURL,
		GpgArgs:   gpgArgs,
		RepoDir:   repoDir,
		Verbose:   verbose,
	}
	return o.addRemote(opts, verbose)
}

// AddRemoteToSysroot adds a remote to an ostree sysroot.
func (o *Ostree) AddRemoteWithSysroot(sysroot string, verbose bool) error {
	gpgArgs, err := o.ClientSideGpgArgs()
	if err != nil {
		return err
	}

	remote, err := o.Remote()
	if err != nil {
		return err
	}
	remoteURL, err := o.RemoteURL()
	if err != nil {
		return err
	}

	opts := AddRemoteOptions{
		Remote:    remote,
		RemoteURL: remoteURL,
		GpgArgs:   gpgArgs,
		Sysroot:   sysroot,
		Verbose:   verbose,
	}
	return o.addRemote(opts, verbose)
}

// LocalRefs lists the locally available ostree refs.
func (o *Ostree) LocalRefs(verbose bool) ([]string, error) {
	repoDir, err := o.RepoDir()
	if err != nil {
		return nil, err
	}
	return o.listLocalRefsFromRepo(repoDir, verbose)
}

// RemoteRefs lists the remote available ostree refs.
func (o *Ostree) RemoteRefs(verbose bool) ([]string, error) {
	repoDir, err := o.RepoDir()
	if err != nil {
		return nil, err
	}
	remote, err := o.Remote()
	if err != nil {
		return nil, err
	}
	return o.listRemoteRefsFromRepo(repoDir, remote, verbose)
}

// ListRootRemoteRefs lists the remote available ostree refs in the root filesystem.
func (o *Ostree) ListRootRemoteRefs(verbose bool) ([]string, error) {
	root, err := o.Root()
	if err != nil {
		return nil, err
	}
	repoDir := filepath.Join(root, "ostree", "repo")
	remote, err := o.Remote()
	if err != nil {
		return nil, err
	}
	return o.listRemoteRefsFromRepo(repoDir, remote, verbose)
}

// ListRootDeployments lists the deployments in the / filesystem.
func (o *Ostree) ListRootDeployments(verbose bool) ([]Deployment, error) {
	root, err := o.Root()
	if err != nil {
		return nil, err
	}
	return o.listDeploymentsFromSysroot(root, verbose)
}

// ListDeploymentsInRoot lists the deployments in the given root,
// which is usually used for chroot operations.
func (o *Ostree) ListDeploymentsInRoot(root string, verbose bool) ([]Deployment, error) {
	return o.listDeploymentsFromSysroot(root, verbose)
}

// DeployedRootfs returns the path to the deployed rootfs.
func (o *Ostree) DeployedRootfs(ref string, verbose bool) (string, error) {
	sysroot, err := o.Sysroot()
	if err != nil {
		return "", err
	}

	if ref == "" {
		return "", errors.New("invalid ref parameter")
	}
	osName, err := o.OsName()
	if err != nil {
		return "", err
	}

	ostreeCommit, err := o.LastCommit(ref, verbose)
	if err != nil {
		return "", fmt.Errorf("cannot get last ostree commit: %w", err)
	}

	rootfs := BuildDeploymentRootfs(sysroot, osName, ostreeCommit, 0)
	return rootfs, nil
}

// BootedRef returns the ref of the booted deployment.
func (o *Ostree) BootedRef(verbose bool) (string, error) {
	root, err := o.Root()
	if err != nil {
		return "", err
	}
	deployments, err := o.listDeploymentsFromSysroot(root, verbose)
	if err != nil {
		return "", err
	}
	for _, d := range deployments {
		if d.Booted {
			return d.Refspec, nil
		}
	}
	return "", errors.New("no booted deployment found")
}

// BootedHash returns the commit hash of the booted deployment.
func (o *Ostree) BootedHash(verbose bool) (string, error) {
	root, err := o.Root()
	if err != nil {
		return "", err
	}
	deployments, err := o.listDeploymentsFromSysroot(root, verbose)
	if err != nil {
		return "", err
	}
	for _, d := range deployments {
		if d.Booted {
			return d.Checksum, nil
		}
	}
	return "", errors.New("no booted deployment found")
}

func (o *Ostree) prepareVarHome(imageDir, homeName, varHomeName string) error {
	homeDir := filepath.Join(imageDir, homeName)
	varHomeDir := filepath.Join(imageDir, "var", varHomeName)

	homeInfo, err := os.Lstat(homeDir)
	homeExists := err == nil

	if homeExists && (homeInfo.Mode()&os.ModeSymlink != 0) {
		if info, err := os.Stat(varHomeDir); err == nil && info.IsDir() {
			link, _ := os.Readlink(homeDir)
			if strings.HasSuffix(link, "var/"+varHomeName) {
				fmt.Printf("%s is a symlink and %s is a directory. All good.\n", homeDir, varHomeDir)
			} else {
				fmt.Fprintf(
					os.Stderr,
					"%s symlink points to an unexpected path: %s\n",
					homeDir,
					link,
				)
				return fmt.Errorf("home symlink invalid")
			}
		}
	} else if homeExists && homeInfo.IsDir() {
		if pathExists(varHomeDir) { // path exists is correct.
			fmt.Println("WARNING: removing " + varHomeDir)
			os.RemoveAll(varHomeDir)
		}
		if err := os.Rename(homeDir, varHomeDir); err != nil {
			return fmt.Errorf("failed to move home: %w", err)
		}
	} else if homeExists {
		if err := os.Remove(homeDir); err != nil {
			return fmt.Errorf("failed to remove home: %w", err)
		}
	}
	if _, err := os.Stat(varHomeDir); os.IsNotExist(err) {
		if err := os.MkdirAll(varHomeDir, 0755); err != nil {
			return fmt.Errorf("failed to create %v: %w", varHomeDir, err)
		}
	}
	// && !os.IsExist(err) done because of the complexity of the conditions above.
	if err := os.Symlink("var/"+varHomeName, homeDir); err != nil && !os.IsExist(err) {
		return fmt.Errorf("failed to symlink %v: %w", homeDir, err)
	}
	return nil
}

// moveDirToTargetAndSymlink moves srcDir to targetDir (if srcDir exists as a real
// directory or removes it if it's a non-directory), ensures targetDir exists, and
// creates a symlink at srcDir pointing to symlinkTarget.
func moveDirToTargetAndSymlink(srcDir, targetDir, symlinkTarget string) error {
	if info, err := os.Lstat(srcDir); err == nil {
		if info.IsDir() {
			if pathExists(targetDir) {
				os.RemoveAll(targetDir)
			}
			fmt.Fprintf(os.Stderr, "WARNING: moving %s to %s.\n", srcDir, targetDir)
			if err := os.Rename(srcDir, targetDir); err != nil {
				return fmt.Errorf("failed to move %s: %w", srcDir, err)
			}
		} else {
			if err := os.Remove(srcDir); err != nil {
				return fmt.Errorf("failed to remove %s: %w", srcDir, err)
			}
		}
	}

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", targetDir, err)
	}

	if err := os.Symlink(symlinkTarget, srcDir); err != nil && !os.IsExist(err) {
		return fmt.Errorf("failed to symlink %s: %w", srcDir, err)
	}
	return nil
}

// prepareSysrootAndOstreeLink creates the /sysroot directory and the
// /ostree -> sysroot/ostree symlink inside imageDir.
func prepareSysrootAndOstreeLink(imageDir string) error {
	if err := os.Mkdir(filepath.Join(imageDir, "sysroot"), 0755); err != nil {
		return fmt.Errorf("failed to create sysroot: %w", err)
	}

	ostreeLink := filepath.Join(imageDir, "ostree")
	if _, err := os.Lstat(ostreeLink); err == nil {
		if err := os.Remove(ostreeLink); err != nil {
			return fmt.Errorf("failed to remove existing ostree link: %w", err)
		}
	}
	if err := os.Symlink("sysroot/ostree", ostreeLink); err != nil {
		return fmt.Errorf("failed to symlink ostree: %w", err)
	}
	return nil
}

// prepareTmpDir moves /tmp into /sysroot/tmp and replaces it with a symlink.
func prepareTmpDir(imageDir string) error {
	tmpDir := filepath.Join(imageDir, "tmp")
	sysrootTmp := filepath.Join(imageDir, "sysroot", "tmp")

	// Move tmpDir only if it exists as a real directory (not a symlink).
	if info, err := os.Lstat(tmpDir); err == nil && info.IsDir() && (info.Mode()&os.ModeSymlink == 0) {
		if err := os.Rename(tmpDir, sysrootTmp); err != nil {
			return fmt.Errorf("failed to move tmp to sysroot/tmp: %w", err)
		}
	}

	if _, err := os.Lstat(tmpDir); err == nil {
		os.Remove(tmpDir)
	}
	if err := os.Symlink("sysroot/tmp", tmpDir); err != nil {
		return fmt.Errorf("failed to symlink tmp: %w", err)
	}
	return nil
}

// prepareMachineID resets /etc/machine-id to an empty file.
func prepareMachineID(imageDir string) error {
	machineID := filepath.Join(imageDir, "etc", "machine-id")
	_ = os.Remove(machineID)
	f, err := os.Create(machineID)
	if err != nil {
		return fmt.Errorf("failed to touch machine-id: %w", err)
	}
	f.Close()
	return nil
}

// prepareVarDbPkg moves var/db/pkg to the read-only VDB location and creates
// a relative symlink back.
func prepareVarDbPkg(imageDir, roVdbPath string) error {
	fmt.Println("Setting up /var/db/pkg...")
	varDbPkg := filepath.Join(imageDir, "var", "db", "pkg")
	usrVarDbPkg := filepath.Join(imageDir, roVdbPath)

	fmt.Printf("Moving %s to %s\n", varDbPkg, usrVarDbPkg)
	if err := os.MkdirAll(filepath.Dir(usrVarDbPkg), 0755); err != nil {
		return fmt.Errorf("failed to create parent of usrVarDbPkg: %w", err)
	}
	if err := os.Rename(varDbPkg, usrVarDbPkg); err != nil {
		return fmt.Errorf("failed to move var/db/pkg: %w", err)
	}

	if err := os.Symlink(filepath.Join("..", "..", roVdbPath), varDbPkg); err != nil {
		return fmt.Errorf("failed to symlink var/db/pkg: %w", err)
	}
	return nil
}

// prepareOpt moves /opt to /usr/opt and symlinks it.
func prepareOpt(imageDir string) error {
	fmt.Println("Setting up /opt...")
	return moveDirToTargetAndSymlink(
		filepath.Join(imageDir, "opt"),
		filepath.Join(imageDir, "usr", "opt"),
		"usr/opt",
	)
}

// prepareSrv moves /srv to /var/srv and symlinks it.
func prepareSrv(imageDir string) error {
	fmt.Println("Setting up /srv...")
	return moveDirToTargetAndSymlink(
		filepath.Join(imageDir, "srv"),
		filepath.Join(imageDir, "var", "srv"),
		"var/srv",
	)
}

// prepareStaticDirs creates /lab, /snap, and /usr/src directories.
func prepareStaticDirs(imageDir string) error {
	dirs := []struct {
		path string
		desc string
	}{
		{"lab", "Setting up /lab (for everything homelabbing and your LAN)..."},
		{"snap", "Setting up /snap ..."},
		{filepath.Join("usr", "src"), "Setting up /usr/src (for snap) ..."},
	}
	for _, d := range dirs {
		fmt.Println(d.desc)
		if err := os.MkdirAll(filepath.Join(imageDir, d.path), 0755); err != nil {
			return fmt.Errorf("failed to create %s: %w", d.path, err)
		}
	}
	return nil
}

// prepareUsrLocal moves /usr/local to /var/usrlocal and symlinks it.
func prepareUsrLocal(imageDir string) error {
	fmt.Println("Setting up /usr/local...")
	usrLocalDir := filepath.Join(imageDir, "usr", "local")
	relUsrLocal := "var/usrlocal"
	imageUsrLocal := filepath.Join(imageDir, relUsrLocal)

	if pathExists(usrLocalDir) {
		if err := os.Rename(usrLocalDir, imageUsrLocal); err != nil {
			return fmt.Errorf("failed to move usr/local: %w", err)
		}
	} else {
		os.MkdirAll(imageUsrLocal, 0755)
	}
	if err := os.Symlink(filepath.Join("..", relUsrLocal), usrLocalDir); err != nil {
		return fmt.Errorf("failed to symlink usr/local: %w", err)
	}
	return nil
}

// PrepareFilesystemHierarchy prepares the filesystem hierarchy for OSTree.
// It ports the logic from ostree_lib.prepare_filesystem_hierarchy in ostree_lib.sh.
func (o *Ostree) PrepareFilesystemHierarchy(imageDir string) error {
	marker := filepath.Join(imageDir, "var", ".matrixos-prepared")
	if fileExists(marker) {
		return fmt.Errorf("filesystem hierarchy already prepared: %s exists", marker)
	}

	if err := prepareSysrootAndOstreeLink(imageDir); err != nil {
		return err
	}

	if err := prepareTmpDir(imageDir); err != nil {
		return err
	}

	if err := prepareMachineID(imageDir); err != nil {
		return err
	}

	if err := o.SetupEtc(imageDir); err != nil {
		return err
	}

	matrixOsRoVdb, err := o.cfg.GetItem("Releaser.ReadOnlyVdb")
	if err != nil {
		return err
	}
	if matrixOsRoVdb == "" {
		return fmt.Errorf("config item Releaser.ReadOnlyVdb is not set")
	}
	if err := prepareVarDbPkg(imageDir, matrixOsRoVdb); err != nil {
		return err
	}

	if err := prepareOpt(imageDir); err != nil {
		return err
	}

	if err := prepareSrv(imageDir); err != nil {
		return err
	}

	if err := prepareStaticDirs(imageDir); err != nil {
		return err
	}

	fmt.Println("Setting up /home ...")
	if err := o.prepareVarHome(imageDir, "home", "home"); err != nil {
		return err
	}
	fmt.Println("Setting up /root ...")
	if err := o.prepareVarHome(imageDir, "root", "roothome"); err != nil {
		return err
	}

	efiRoot, err := o.cfg.GetItem("Imager.EfiRoot")
	if err != nil {
		return err
	}
	if efiRoot == "" {
		return fmt.Errorf("config item Imager.EfiRoot is not set")
	}
	fmt.Printf("Setting up %s...\n", efiRoot)
	os.MkdirAll(filepath.Join(imageDir, efiRoot), 0755)

	if err := prepareUsrLocal(imageDir); err != nil {
		return err
	}

	if err := os.WriteFile(marker, []byte("prepared"), 0644); err != nil {
		return fmt.Errorf("failed to create marker file: %w", err)
	}

	return nil
}

// ValidateFilesystemHierarchy validates the filesystem hierarchy for OSTree.
func (o *Ostree) ValidateFilesystemHierarchy(imageDir string) error {
	if imageDir == "" {
		return errors.New("missing imageDir parameter")
	}

	expected := []string{
		"/home",
		"/opt",
		"/root",
		"/srv",
		"/tmp",
		"/usr/local",
	}

	var issues int
	for _, relPath := range expected {
		fullPath := filepath.Join(imageDir, relPath)

		// Check if it's a symlink and if it points to a directory.
		// We use Lstat to check the link itself and Stat to check the target.
		lfi, lerr := os.Lstat(fullPath)
		if lerr == nil && lfi.Mode()&os.ModeSymlink != 0 {
			if fi, err := os.Stat(fullPath); err == nil && fi.IsDir() {
				continue
			}
		}

		fmt.Fprintf(os.Stderr, "Expected %s to be a symlink to a directory.\n",
			fullPath)
		fmt.Fprintln(os.Stderr, "Please check the filesystem hierarchy.")
		issues++
	}

	if issues > 0 {
		return fmt.Errorf("filesystem hierarchy validation failed: %d issues",
			issues)
	}

	return nil
}

// Switch runs `ostree admin switch` to switch to the given ref.
func (o *Ostree) Switch(ref string, verbose bool) error {
	sysroot, err := o.Sysroot()
	if err != nil {
		return err
	}
	return o.ostreeRun(verbose, "admin", "switch", "--sysroot="+sysroot, ref)
}

// Deploy deploys an ostree commit.
func (o *Ostree) Deploy(ref string, bootArgs []string, verbose bool) error {
	sysroot, err := o.Sysroot()
	if err != nil {
		return err
	}
	repoDir, err := o.RepoDir()
	if err != nil {
		return err
	}
	remote, err := o.Remote()
	if err != nil {
		return err
	}

	fmt.Printf("Creating %s ...\n", sysroot)
	if err := os.MkdirAll(sysroot, 0755); err != nil {
		return err
	}

	ostreeCommit, err := o.lastCommitFromRepo(repoDir, ref, verbose)
	if err != nil {
		return fmt.Errorf("cannot get last ostree commit: %w", err)
	}

	fmt.Printf("Initializing ostree dir structure into %s ...\n", sysroot)
	if err := o.ostreeRun(verbose, "admin", "init-fs", sysroot); err != nil {
		return err
	}

	osName, err := o.OsName()
	if err != nil {
		return err
	}

	fmt.Println("ostree os-init ...")
	if err := o.ostreeRun(verbose, "admin", "os-init", osName, "--sysroot="+sysroot); err != nil {
		return err
	}

	sysrootRepo := filepath.Join(sysroot, "ostree", "repo")
	fmt.Println("ostree pull-local ...")
	if err := o.ostreeRun(verbose, "pull-local", "--repo="+sysrootRepo, repoDir, ostreeCommit); err != nil {
		return err
	}
	if err := o.ostreeRun(verbose, "refs", "--repo="+sysrootRepo, "--create="+remote+":"+ref, ostreeCommit); err != nil {
		return err
	}

	fmt.Println("ostree setting bootloader to none (using blscfg instead) ...")
	if err := o.ostreeRun(verbose, "config", "--repo="+sysrootRepo, "set", "sysroot.bootloader", "none"); err != nil {
		return err
	}

	fmt.Println("ostree setting bootprefix = false, given separate boot partition ...")
	if err := o.ostreeRun(verbose, "config", "--repo="+sysrootRepo, "set", "sysroot.bootprefix", "false"); err != nil {
		return err
	}

	fmt.Println("ostree admin deploy ...")
	deployArgs := []string{
		"admin", "deploy",
		"--sysroot=" + sysroot,
		"--os=" + osName,
	}
	for _, ba := range bootArgs {
		deployArgs = append(deployArgs, "--karg-append="+ba)
	}
	deployArgs = append(deployArgs, remote+":"+ref)

	if err := o.ostreeRun(verbose, deployArgs...); err != nil {
		return err
	}

	fmt.Printf("ostree commit deployed: %s.\n", ostreeCommit)
	return nil
}

// Upgrade runs `ostree admin upgrade`.
func (o *Ostree) Upgrade(args []string, verbose bool) error {
	root, err := o.Root()
	if err != nil {
		return err
	}

	cmdArgs := []string{"admin", "upgrade", "--sysroot=" + root}
	cmdArgs = append(cmdArgs, args...)

	return o.ostreeRun(verbose, cmdArgs...)
}

// ParseModeString takes a hybrid string like "-00644" and parses it.
func ParseModeString(input string) (*fslib.PathMode, error) {
	if len(input) < 4 {
		return nil, fmt.Errorf("input too short to be valid mode string: %q", input)
	}

	mode := fslib.PathMode{
		Type: string(input[0]),
	}

	// Extract the octal portion.
	// strconv.ParseUint inherently understands base 8 if we specify it.
	rawPerms, err := strconv.ParseUint(input[1:], 8, 32)
	if err != nil {
		return nil, fmt.Errorf("failed to parse octal permissions: %w", err)
	}

	// Define POSIX bitmasks (using Go's 0o prefix for octal literals)
	const (
		posixSetUID = 0o4000
		posixSetGID = 0o2000
		posixSticky = 0o1000
		posixPerms  = 0o0777 // Mask for standard rwxrwxrwx
	)

	// Extract special bits via bitwise AND
	mode.SetUID = (rawPerms & posixSetUID) != 0
	mode.SetGID = (rawPerms & posixSetGID) != 0
	mode.Sticky = (rawPerms & posixSticky) != 0

	// Extract standard permissions
	mode.Perms = fs.FileMode(rawPerms & posixPerms)

	return &mode, nil
}

// ParseOstreeLsChecksumLine parses a line from `ostree ls -C` output into a PathInfo struct.
func ParseOstreeLsChecksumLine(line string) (*fslib.PathInfo, error) {
	parts := strings.Fields(line)
	if len(parts) < 6 {
		return nil, fmt.Errorf("unexpected format for ostree ls line: %q", line)
	}
	idx := 0

	pi := &fslib.PathInfo{}
	mode, err := ParseModeString(parts[idx])
	if err != nil {
		return nil, err
	}
	pi.Mode = mode
	idx++

	uid, gid := parts[idx], parts[idx+1]
	idx += 2

	pi.Uid, err = strconv.ParseUint(uid, 10, 32)
	if err != nil {
		return nil, err
	}

	pi.Gid, err = strconv.ParseUint(gid, 10, 32)
	if err != nil {
		return nil, err
	}

	pi.Size, err = strconv.ParseUint(parts[idx], 10, 64)
	if err != nil {
		return nil, err
	}
	idx++

	if pi.Mode.Type == "d" {
		// Directories have two checksums, use the second one.
		idx++
	}

	pi.OSTreeChecksum = parts[idx]
	idx++

	pi.Path = parts[idx]
	idx++
	if pi.Mode.Type == "l" && len(parts) >= 8 {
		idx++
		pi.Link = parts[idx]
	}
	return pi, nil
}

// ListContents lists the contents of a path in a commit.
func (o *Ostree) ListContents(commit, path string, verbose bool) (*[]fslib.PathInfo, error) {
	if commit == "" {
		return nil, errors.New("missing commit parameter")
	}
	if path == "" {
		return nil, errors.New("missing path parameter")
	}
	repoDir, err := o.RepoDir()
	if err != nil {
		return nil, err
	}
	return o.listContentsOfPath(commit, repoDir, path, verbose)
}

// ListContentsInRoot lists the contents of a path in a commit
// using the ostree repo in the given root.
func (o *Ostree) ListContentsInRoot(commit, path string, verbose bool) (*[]fslib.PathInfo, error) {
	if commit == "" {
		return nil, errors.New("missing commit parameter")
	}
	if path == "" {
		return nil, errors.New("missing path parameter")
	}
	root, err := o.Root()
	if err != nil {
		return nil, err
	}
	repoDir := filepath.Join(root, "ostree", "repo")
	return o.listContentsOfPath(commit, repoDir, path, verbose)
}

func (o *Ostree) listContentsOfPath(commit, repoDir, path string, verbose bool) (*[]fslib.PathInfo, error) {
	stdout, err := o.ostreeRunCapture(
		verbose,
		"--repo="+repoDir,
		"ls",
		"-C",
		"-R",
		commit,
		"--",
		path,
	)
	if err != nil {
		return nil, err
	}

	var pis []fslib.PathInfo

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		pi, err := ParseOstreeLsChecksumLine(line)
		if err != nil {
			return nil, err
		}
		pis = append(pis, *pi)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return &pis, nil
}

// EtcChangeAction describes what will happen to a file in /etc during merge.
type EtcChangeAction string

// EtcChange describes a single change detected by the 3-way /etc diff.
type EtcChange struct {
	Path   string          // Relative path within /etc (e.g. "conf.d/foo")
	Action EtcChangeAction // What will happen to this path
	Old    *fslib.PathInfo // State in old commit (nil if absent)
	New    *fslib.PathInfo // State in new commit (nil if absent)
	User   *fslib.PathInfo // Current live state (nil if absent)
}

// indexPathInfoSlice builds a map from relative path to *PathInfo.
// It strips the given prefix from each entry's Path and skips the root
// directory itself (empty relative path after stripping).
func indexPathInfoSlice(items *[]fslib.PathInfo, prefix string) map[string]*fslib.PathInfo {
	if items == nil {
		return map[string]*fslib.PathInfo{}
	}
	m := make(map[string]*fslib.PathInfo, len(*items))
	for i := range *items {
		pi := &(*items)[i]
		rel := strings.TrimPrefix(pi.Path, prefix)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			continue
		}
		m[rel] = pi
	}
	return m
}

// indexPathInfoPtrSlice is like indexPathInfoSlice but for []*PathInfo.
func indexPathInfoPtrSlice(items []*fslib.PathInfo, prefix string) map[string]*fslib.PathInfo {
	m := make(map[string]*fslib.PathInfo, len(items))
	for _, pi := range items {
		rel := strings.TrimPrefix(pi.Path, prefix)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			continue
		}
		m[rel] = pi
	}
	return m
}

// computeEtcDiff performs a 3-way diff between the old pristine /usr/etc,
// the new pristine /usr/etc, and the user's live /etc.
//
// The algorithm keys every entry by its relative path (e.g. "conf.d/foo")
// and classifies each path into one of the EtcChangeAction categories.
func computeEtcDiff(
	oldContent *[]fslib.PathInfo,
	newContent *[]fslib.PathInfo,
	userContent []*fslib.PathInfo,
) []EtcChange {
	oldMap := indexPathInfoSlice(oldContent, "/usr/etc")
	newMap := indexPathInfoSlice(newContent, "/usr/etc")
	userMap := indexPathInfoPtrSlice(userContent, "/etc")

	// Collect every unique relative path.
	allPaths := make(map[string]struct{})
	for k := range oldMap {
		allPaths[k] = struct{}{}
	}
	for k := range newMap {
		allPaths[k] = struct{}{}
	}
	for k := range userMap {
		allPaths[k] = struct{}{}
	}

	var changes []EtcChange
	for relPath := range allPaths {
		change := classifyEtcChange(relPath, oldMap[relPath], newMap[relPath], userMap[relPath])
		if change != nil {
			changes = append(changes, *change)
		}
	}

	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Path < changes[j].Path
	})
	return changes
}

// classifyEtcChange determines the action for a single path given its state
// in the old commit, new commit, and user's live filesystem.
//
// Truth table (✓ = present, ✗ = absent):
//
//	old   new   user  | result
//	───── ───── ───── | ─────────────────────────────────────────────
//	 ✓     ✓     ✓   | old==new && old==user → skip (unchanged)
//	                  | old==new && old!=user → user-only
//	                  | old!=new && old==user → update
//	                  | old!=new && old!=user → conflict (unless new==user → skip)
//	 ✗     ✓     ✗   | add
//	 ✗     ✓     ✓   | new==user → skip, else conflict
//	 ✓     ✗     ✓   | old==user → remove, else conflict
//	 ✓     ✗     ✗   | skip (both removed)
//	 ✓     ✓     ✗   | old==new → user-only, else conflict
//	 ✗     ✗     ✓   | user-only
func classifyEtcChange(relPath string, old, new_, user *fslib.PathInfo) *EtcChange {
	hasOld := old != nil
	hasNew := new_ != nil
	hasUser := user != nil

	switch {
	case hasOld && hasNew && hasUser:
		oldEqNew := old.Equals(new_)
		oldEqUser := old.Equals(user)

		switch {
		case oldEqNew && oldEqUser:
			return nil // unchanged everywhere
		case oldEqNew:
			// upstream unchanged, user modified
			return &EtcChange{Path: relPath, Action: EtcActionUserOnly, Old: old, New: new_, User: user}
		case oldEqUser:
			// upstream modified, user unchanged
			return &EtcChange{Path: relPath, Action: EtcActionUpdate, Old: old, New: new_, User: user}
		default:
			// both modified
			if new_.Equals(user) {
				return nil // converged to the same state
			}
			return &EtcChange{Path: relPath, Action: EtcActionConflict, Old: old, New: new_, User: user}
		}

	case !hasOld && hasNew && !hasUser:
		// upstream added, user doesn't have it
		return &EtcChange{Path: relPath, Action: EtcActionAdd, New: new_}

	case !hasOld && hasNew && hasUser:
		// upstream added AND user has it
		if new_.Equals(user) {
			return nil
		}
		return &EtcChange{Path: relPath, Action: EtcActionConflict, New: new_, User: user}

	case hasOld && !hasNew && hasUser:
		// upstream removed, user still has it
		if old.Equals(user) {
			return &EtcChange{Path: relPath, Action: EtcActionRemove, Old: old, User: user}
		}
		return &EtcChange{Path: relPath, Action: EtcActionConflict, Old: old, User: user}

	case hasOld && !hasNew && !hasUser:
		// both removed
		return nil

	case hasOld && hasNew && !hasUser:
		// user removed it
		if old.Equals(new_) {
			return &EtcChange{Path: relPath, Action: EtcActionUserOnly, Old: old, New: new_}
		}
		// upstream changed, user removed → conflict
		return &EtcChange{Path: relPath, Action: EtcActionConflict, Old: old, New: new_}

	case !hasOld && !hasNew && hasUser:
		// user added, not in old or new
		return &EtcChange{Path: relPath, Action: EtcActionUserOnly, User: user}

	default:
		return nil
	}
}

// ListEtcChanges performs a 3-way diff between the old pristine /usr/etc,
// the new pristine /usr/etc, and the user's live /etc, and returns a list of
// changes with their classification (add/update/remove/conflict/user-only).
func (o *Ostree) ListEtcChanges(oldSHA, newSHA string) ([]EtcChange, error) {
	oldEtcContent, err := o.ListContentsInRoot(oldSHA, "/usr/etc", false)
	if err != nil {
		return nil, err
	}
	newEtcContent, err := o.ListContentsInRoot(newSHA, "/usr/etc", false)
	if err != nil {
		return nil, err
	}
	userEtcContent, err := fslib.ListContents("/etc")
	if err != nil {
		return nil, err
	}

	changes := computeEtcDiff(oldEtcContent, newEtcContent, userEtcContent)
	return changes, nil
}

// ListPackages lists the packages in a commit.
func (o *Ostree) ListPackages(commit string, verbose bool) ([]string, error) {
	if commit == "" {
		return nil, errors.New("missing commit parameter")
	}
	root, err := o.Root()
	if err != nil {
		return nil, err
	}

	roVdb, err := o.cfg.GetItem("Releaser.ReadOnlyVdb")
	if err != nil {
		return nil, err
	}
	if roVdb == "" {
		return nil, fmt.Errorf("config item Releaser.ReadOnlyVdb is not set")
	}

	pkgs, err := o.listPackagesFromPath(root, roVdb, commit, verbose)
	if err == nil && len(pkgs) > 0 {
		return pkgs, nil
	}
	return o.listPackagesFromPath(root, "/var/db/pkg", commit, verbose)
}

func (o *Ostree) listPackagesFromPath(root, path, commit string, verbose bool) ([]string, error) {
	repoDir := filepath.Join(root, "ostree", "repo")
	vardbpkg := filepath.Join(root, path)

	stdout, err := o.ostreeRunCapture(
		verbose,
		"--repo="+repoDir,
		"ls",
		"-C",
		"-R",
		commit,
		"--",
		vardbpkg,
	)
	if err != nil {
		return nil, err
	}

	var pkgs []string

	prefix := vardbpkg
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()

		pi, err := ParseOstreeLsChecksumLine(line)
		if err != nil {
			return nil, err
		}

		if pi.Mode.Type != "d" {
			continue
		}
		if !strings.HasPrefix(pi.Path, prefix) {
			continue
		}

		relPath := strings.TrimPrefix(pi.Path, prefix)
		relPath = strings.TrimSuffix(relPath, "/")

		if strings.Count(relPath, "/") == 1 {
			pkgs = append(pkgs, relPath)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	sort.Strings(pkgs)
	return pkgs, nil
}

// ConfigDiff runs "ostree admin --sysroot=<root> config-diff" and returns a
// map whose keys are the status letter (e.g. "A", "M", "D") and whose values
// are sorted slices of paths that have that status.
func (o *Ostree) ConfigDiff(verbose bool) (map[string][]string, error) {
	root, err := o.Root()
	if err != nil {
		return nil, err
	}

	stdout, err := o.ostreeRunCapture(
		verbose,
		"admin",
		"--sysroot="+root,
		"config-diff",
	)
	if err != nil {
		return nil, err
	}

	result := make(map[string][]string)

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		status := fields[0]
		path := fields[1]
		result[status] = append(result[status], path)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	for key := range result {
		sort.Strings(result[key])
	}

	return result, nil
}
