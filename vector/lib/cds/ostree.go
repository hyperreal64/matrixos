package cds

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"matrixos/vector/lib/config"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

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

// LastCommitWithSysroot returns the last commit for a given ref in a sysroot.
func LastCommitWithSysroot(sysroot, ref string, verbose bool) (string, error) {
	if sysroot == "" {
		return "", errors.New("invalid sysroot parameter")
	}

	repoDir := filepath.Join(strings.TrimRight(sysroot, "/"), "ostree", "repo")
	return LastCommit(repoDir, ref, verbose)
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

	rootfs := filepath.Join(sysroot, "ostree", "deploy", osName, "deploy", ostreeCommit+".0")
	return rootfs, nil
}

type Deployment struct {
	Booted   bool   `json:"booted"`
	Checksum string `json:"checksum"`
	// Requires matrixOS ostree-2025.7-r1
	Refspec string `json:"refspec"`
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

	data, err := ostreeAdminStatusJson(sysroot, verbose)
	if err != nil {
		return "", err
	}
	if data == nil {
		return "", errors.New("failed to get ostree status")
	}

	var deployments []Deployment
	if err := json.Unmarshal(*data, &deployments); err != nil {
		return "", fmt.Errorf("failed to unmarshal ostree status: %w", err)
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

	data, err := ostreeAdminStatusJson(sysroot, verbose)
	if err != nil {
		return "", err
	}
	if data == nil {
		return "", errors.New("failed to get ostree status")
	}

	var deployments []Deployment
	if err := json.Unmarshal(*data, &deployments); err != nil {
		return "", fmt.Errorf("failed to unmarshal ostree status: %w", err)
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

type Ostree struct {
	cfg config.IConfig
}

// New creates a new Ostree instance.
func New(cfg config.IConfig) (*Ostree, error) {
	if cfg == nil {
		return nil, errors.New("missing config parameter")
	}
	return &Ostree{
		cfg: cfg,
	}, nil
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
func Run(verbose bool, args ...string) error {
	return run(os.Stdout, os.Stderr, verbose, args...)
}

func RunWithStdoutCapture(verbose bool, args ...string) (io.Reader, error) {
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

// GpgPublicKeyPath returns the user defined /etc/matrixos-private placed
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

// GpgPublicKeyPath returns the user defined /etc/matrixos-private placed
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

// Sysroot returns the path to the ostree sysroot directory.
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
	return ListRemotes(repoDir, verbose)
}

// LastCommit returns the last commit for a given ref.
func (o *Ostree) LastCommit(ref string, verbose bool) (string, error) {
	repoDir, err := o.RepoDir()
	if err != nil {
		return "", err
	}
	return LastCommit(repoDir, ref, verbose)
}

// LastCommitWithSysroot returns the last commit for a given ref in a sysroot.
func (o *Ostree) LastCommitWithSysroot(ref string, verbose bool) (string, error) {
	sysroot, err := o.cfg.GetItem("Ostree.Sysroot")
	if err != nil {
		return "", err
	}
	repoDir := filepath.Join(strings.TrimRight(sysroot, "/"), "ostree", "repo")
	return LastCommit(repoDir, ref, verbose)
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
	err = runCommand(
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

	return runCommand(
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

	err = runCommand(
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

		err := Run(verbose, "--repo="+repoDir, "remote", "gpg-import", remote, "-k", key)
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
		err := Run(verbose, "--repo="+repoDir, "init", "--mode=archive")
		if err != nil {
			return err
		}
	} else {
		fmt.Printf("ostree repo at %v already initialized. Reusing ...\n", repoDir)
	}

	remotes, err := ListRemotes(repoDir, verbose)
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
		err = Run(verbose, args...)
		if err != nil {
			return err
		}
	}

	fmt.Println("Showing current ostree remotes:")
	err = Run(verbose, "--repo="+repoDir, "remote", "list", "-u")
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
	return Pull(repoDir, ref, verbose)
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
	return PullWithRemote(repoDir, remote, ref, verbose)
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
	return Prune(repoDir, ref, keepObjectsYoungerThan, verbose)
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

	stdout, err := RunWithStdoutCapture(
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

	stdout, err = RunWithStdoutCapture(
		verbose,
		"--repo="+repoDir,
		"rev-parse",
		ref+"^",
	)
	if err != nil {
		// This is not a fatal error, the branch might not have a previous commit.
	}
	revOld, _ := readerToFirstNonEmptyLine(stdout)

	args := []string{
		"--repo=" + repoDir,
		"static-delta", "generate",
		"--to=" + revNew,
		"--inline",
		"--min-fallback-size=0",
		"--disable-bsdiff",
	}

	if revOld == "" {
		args = append(args, "--empty")
	} else {
		args = append(args, "--from="+revOld)
	}

	return Run(verbose, args...)
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

	return Run(verbose, args...)
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
	return AddRemoteWithOptions(opts, verbose)
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
	return AddRemoteWithOptions(opts, verbose)
}

// LocalRefs lists the locally available ostree refs.
func (o *Ostree) LocalRefs(verbose bool) ([]string, error) {
	repoDir, err := o.RepoDir()
	if err != nil {
		return nil, err
	}
	return ListLocalRefs(repoDir, verbose)
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
	return ListRemoteRefs(repoDir, remote, verbose)
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

	rootfs := filepath.Join(sysroot, "ostree", "deploy", osName, "deploy", ostreeCommit+".0")
	return rootfs, nil
}

// BootedRef returns the ref of the booted deployment.
func (o *Ostree) BootedRef(verbose bool) (string, error) {
	sysroot, err := o.Sysroot()
	if err != nil {
		return "", err
	}
	return BootedRefWithSysroot(sysroot, verbose)
}

// BootedHash returns the commit hash of the booted deployment.
func (o *Ostree) BootedHash(verbose bool) (string, error) {
	sysroot, err := o.Sysroot()
	if err != nil {
		return "", err
	}
	return BootedHashWithSysroot(sysroot, verbose)
}

// PrepareFilesystemHierarchy prepares the filesystem hierarchy for OSTree.
// It ports the logic from ostree_lib.prepare_filesystem_hierarchy in ostree_lib.sh.
func (o *Ostree) PrepareFilesystemHierarchy(imageDir string) error {
	marker := filepath.Join(imageDir, "var", ".matrixos-prepared")
	if fileExists(marker) {
		return fmt.Errorf("filesystem hierarchy already prepared: %s exists", marker)
	}

	// The image dir must contain /sysroot
	if err := os.Mkdir(filepath.Join(imageDir, "sysroot"), 0755); err != nil {
		return fmt.Errorf("failed to create sysroot: %w", err)
	}

	// ln -s sysroot/ostree "${imagedir}/ostree"
	ostreeLink := filepath.Join(imageDir, "ostree")
	if _, err := os.Lstat(ostreeLink); err == nil {
		if err := os.Remove(ostreeLink); err != nil {
			return fmt.Errorf("failed to remove existing ostree link: %w", err)
		}
	}
	if err := os.Symlink("sysroot/ostree", ostreeLink); err != nil {
		return fmt.Errorf("failed to symlink ostree: %w", err)
	}

	// mv "${imagedir}/tmp" "${imagedir}/sysroot/tmp"
	tmpDir := filepath.Join(imageDir, "tmp")
	sysrootTmp := filepath.Join(imageDir, "sysroot", "tmp")

	// Check if tmpDir exists and is NOT a symlink (to avoid moving an existing symlink into sysroot)
	if info, err := os.Lstat(tmpDir); err == nil && info.IsDir() && (info.Mode()&os.ModeSymlink == 0) {
		if err := os.Rename(tmpDir, sysrootTmp); err != nil {
			return fmt.Errorf("failed to move tmp to sysroot/tmp: %w", err)
		}
	}

	// ln -s "sysroot/tmp" "${imagedir}/tmp"
	if _, err := os.Lstat(tmpDir); err == nil {
		os.Remove(tmpDir)
	}
	if err := os.Symlink("sysroot/tmp", tmpDir); err != nil {
		return fmt.Errorf("failed to symlink tmp: %w", err)
	}

	// Clean up /etc/machine-id
	machineID := filepath.Join(imageDir, "etc", "machine-id")
	_ = os.Remove(machineID)
	if f, err := os.Create(machineID); err != nil {
		return fmt.Errorf("failed to touch machine-id: %w", err)
	} else {
		f.Close()
	}

	// setup_etc
	if err := o.SetupEtc(imageDir); err != nil {
		return err
	}

	fmt.Println("Setting up /var/db/pkg...")
	varDbPkg := filepath.Join(imageDir, "var", "db", "pkg")

	matrixOsRoVdb, err := o.cfg.GetItem("Releaser.ReadOnlyVdb")
	if err != nil {
		return err
	}
	if matrixOsRoVdb == "" {
		return fmt.Errorf("config item Releaser.ReadOnlyVdb is not set")
	}

	usrVarDbPkg := filepath.Join(imageDir, matrixOsRoVdb)

	fmt.Printf("Moving %s to %s\n", varDbPkg, usrVarDbPkg)
	// Ensure parent exists
	if err := os.MkdirAll(filepath.Dir(usrVarDbPkg), 0755); err != nil {
		return fmt.Errorf("failed to create parent of usrVarDbPkg: %w", err)
	}
	if err := os.Rename(varDbPkg, usrVarDbPkg); err != nil {
		return fmt.Errorf("failed to move var/db/pkg: %w", err)
	}

	// ln -s "../../${relusrvardbpkg}" "${vardbpkg}"
	if err := os.Symlink(filepath.Join("..", "..", matrixOsRoVdb), varDbPkg); err != nil {
		return fmt.Errorf("failed to symlink var/db/pkg: %w", err)
	}

	fmt.Println("Setting up /opt...")
	optDir := filepath.Join(imageDir, "opt")
	imageOptDir := filepath.Join(imageDir, "usr", "opt")

	if info, err := os.Lstat(optDir); err == nil {
		if info.IsDir() {
			if pathExists(imageOptDir) { // path exists is correct.
				os.RemoveAll(imageOptDir)
			}
			fmt.Fprintf(os.Stderr, "WARNING: moving %s to %s.\n", optDir, imageOptDir)
			if err := os.Rename(optDir, imageOptDir); err != nil {
				return fmt.Errorf("failed to move opt: %w", err)
			}
		} else {
			if err := os.Remove(optDir); err != nil {
				return fmt.Errorf("failed to remove opt: %w", err)
			}
		}
	}

	// Create /usr/opt in case it's missing entirely.
	if err := os.MkdirAll(imageOptDir, 0755); err != nil {
		return fmt.Errorf("failed to create opt: %w", err)
	}

	// ln -s usr/opt "${imagedir}/opt"
	if err := os.Symlink("usr/opt", optDir); err != nil && !os.IsExist(err) {
		return fmt.Errorf("failed to symlink opt: %w", err)
	}

	fmt.Println("Setting up /lab (for everything homelabbing and your LAN)...")
	labDir := filepath.Join(imageDir, "lab")
	if err := os.MkdirAll(labDir, 0755); err != nil {
		return fmt.Errorf("failed to create lab: %w", err)
	}

	fmt.Println("Setting up /srv...")
	srvDir := filepath.Join(imageDir, "srv")
	varSrvDir := filepath.Join(imageDir, "var", "srv")

	if info, err := os.Lstat(srvDir); err == nil {
		if info.IsDir() {
			if pathExists(varSrvDir) { // path exists is correct.
				os.RemoveAll(varSrvDir)
			}
			fmt.Fprintf(os.Stderr, "WARNING: moving %s to %s.\n", srvDir, varSrvDir)
			if err := os.Rename(srvDir, varSrvDir); err != nil {
				return fmt.Errorf("failed to move srv: %w", err)
			}
		} else {
			if err := os.Remove(srvDir); err != nil {
				return fmt.Errorf("failed to remove srv: %w", err)
			}
		}
	}

	if err := os.MkdirAll(varSrvDir, 0755); err != nil {
		return fmt.Errorf("failed to create var/srv: %w", err)
	}

	if err := os.Symlink("var/srv", srvDir); err != nil {
		return fmt.Errorf("failed to symlink srv: %w", err)
	}

	fmt.Println("Setting up /snap ...")
	if err := os.MkdirAll(filepath.Join(imageDir, "snap"), 0755); err != nil {
		return fmt.Errorf("failed to create snap: %w", err)
	}

	fmt.Println("Setting up /usr/src (for snap) ...")
	if err := os.MkdirAll(filepath.Join(imageDir, "usr", "src"), 0755); err != nil {
		return fmt.Errorf("failed to create usr/src: %w", err)
	}

	fmt.Println("Setting up /home ...")
	homeDir := filepath.Join(imageDir, "home")
	varHomeDir := filepath.Join(imageDir, "var", "home")

	homeInfo, err := os.Lstat(homeDir)
	homeExists := err == nil

	if homeExists && (homeInfo.Mode()&os.ModeSymlink != 0) {
		if info, err := os.Stat(varHomeDir); err == nil && info.IsDir() {
			link, _ := os.Readlink(homeDir)
			if strings.HasSuffix(link, "var/home") {
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
	// && !os.IsExist(err) done because of the complexity of the conditions above.
	if err := os.Symlink("var/home", homeDir); err != nil && !os.IsExist(err) {
		return fmt.Errorf("failed to symlink home: %w", err)
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

	fmt.Println("Setting up /usr/local...")
	usrLocalDir := filepath.Join(imageDir, "usr", "local")
	relUsrLocal := "var/usrlocal"
	imageUsrLocal := filepath.Join(imageDir, relUsrLocal)

	if pathExists(usrLocalDir) { // move it as long as it exists.
		if err := os.Rename(usrLocalDir, imageUsrLocal); err != nil {
			return fmt.Errorf("failed to move usr/local: %w", err)
		}
	} else {
		// Ensure the target directory exists if we didn't move it
		os.MkdirAll(imageUsrLocal, 0755)
	}
	if err := os.Symlink(filepath.Join("..", relUsrLocal), usrLocalDir); err != nil {
		return fmt.Errorf("failed to symlink usr/local: %w", err)
	}

	if err := os.WriteFile(marker, []byte("prepared"), 0644); err != nil {
		return fmt.Errorf("failed to create marker file: %w", err)
	}

	return nil
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

	ostreeCommit, err := LastCommit(repoDir, ref, verbose)
	if err != nil {
		return fmt.Errorf("cannot get last ostree commit: %w", err)
	}

	fmt.Printf("Initializing ostree dir structure into %s ...\n", sysroot)
	if err := Run(verbose, "admin", "init-fs", sysroot); err != nil {
		return err
	}

	osName, err := o.OsName()
	if err != nil {
		return err
	}

	fmt.Println("ostree os-init ...")
	if err := Run(verbose, "admin", "os-init", osName, "--sysroot="+sysroot); err != nil {
		return err
	}

	sysrootRepo := filepath.Join(sysroot, "ostree", "repo")
	fmt.Println("ostree pull-local ...")
	if err := Run(verbose, "pull-local", "--repo="+sysrootRepo, repoDir, ostreeCommit); err != nil {
		return err
	}
	if err := Run(verbose, "refs", "--repo="+sysrootRepo, "--create="+remote+":"+ref, ostreeCommit); err != nil {
		return err
	}

	fmt.Println("ostree setting bootloader to none (using blscfg instead) ...")
	if err := Run(verbose, "config", "--repo="+sysrootRepo, "set", "sysroot.bootloader", "none"); err != nil {
		return err
	}

	fmt.Println("ostree setting bootprefix = false, given separate boot partition ...")
	if err := Run(verbose, "config", "--repo="+sysrootRepo, "set", "sysroot.bootprefix", "false"); err != nil {
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

	if err := Run(verbose, deployArgs...); err != nil {
		return err
	}

	fmt.Printf("ostree commit deployed: %s.\n", ostreeCommit)
	return nil
}

// Upgrade runs `ostree admin upgrade`.
func (o *Ostree) Upgrade(sysroot string, args []string, verbose bool) error {
	if sysroot == "" {
		return errors.New("missing ostree sysroot parameter")
	}

	cmdArgs := []string{"admin", "upgrade"}
	cmdArgs = append(cmdArgs, args...)

	return Run(verbose, cmdArgs...)
}

// ListPackages lists the packages in a commit.
func (o *Ostree) ListPackages(commit, sysroot string, verbose bool) ([]string, error) {
	if commit == "" {
		return nil, errors.New("missing commit parameter")
	}
	if sysroot == "" {
		return nil, errors.New("missing sysroot parameter")
	}

	repoDir := filepath.Join(strings.TrimRight(sysroot, "/"), "ostree", "repo")

	roVdb, err := o.cfg.GetItem("Releaser.ReadOnlyVdb")
	if err != nil {
		return nil, err
	}

	vdb := roVdb
	vardbpkg := filepath.Join(strings.TrimRight(sysroot, "/"), roVdb)
	if !directoryExists(vardbpkg) {
		vardbpkg = filepath.Join(strings.TrimRight(sysroot, "/"), "var", "db", "pkg")
		vdb = "/var/db/pkg"
	}
	if !directoryExists(vardbpkg) {
		return nil, fmt.Errorf("%s does not exist", vardbpkg)
	}

	stdout, err := RunWithStdoutCapture(
		verbose,
		"--repo="+repoDir,
		"ls",
		"-R",
		commit,
		"--",
		vdb,
	)
	if err != nil {
		return nil, err
	}

	var packages []string
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "d") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		path := fields[4]
		// ostree ls output paths are usually relative (no leading slash).
		// vdb config usually has a leading slash. Normalize both.
		path = strings.TrimPrefix(path, "/")
		path = strings.TrimPrefix(path, strings.TrimPrefix(vdb, "/")+"/")

		if strings.Count(path, "/") == 1 {
			packages = append(packages, path)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	sort.Strings(packages)

	return packages, nil
}
