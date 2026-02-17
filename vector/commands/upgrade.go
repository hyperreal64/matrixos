package commands

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode"

	"matrixos/vector/lib/cds"
	"matrixos/vector/lib/config"
)

const (
	cReset  = "\033[0m"
	cRed    = "\033[31m"
	cGreen  = "\033[32m"
	cYellow = "\033[33m"
	cBold   = "\033[1m"
)

var (
	grubEfiBinary = "GRUBX64.EFI"
	bootloaders   = []string{
		grubEfiBinary,
	}
)

// UpgradeCommand is a command for upgrading the system
type UpgradeCommand struct {
	fs            *flag.FlagSet
	cfg           config.IConfig
	ot            *cds.Ostree
	assumeYes     bool
	updBootloader bool
}

// NewUpgradeCommand creates a new UpgradeCommand
func NewUpgradeCommand() ICommand {
	c := &UpgradeCommand{
		fs: flag.NewFlagSet("upgrade", flag.ExitOnError),
	}
	c.fs.BoolVar(&c.updBootloader, "update-bootloader", false, "Update bootloader binaries in /efi")
	c.fs.BoolVar(&c.assumeYes, "y", false, "Assume yes to all prompts")
	return c
}

// Name returns the name of the command
func (c *UpgradeCommand) Name() string {
	return c.fs.Name()
}

func (c *UpgradeCommand) initConfig() error {
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

func (c *UpgradeCommand) initOstree() error {
	ot, err := cds.NewOstree(c.cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize ostree: %w", err)
	}
	c.ot = ot
	return nil
}

// Init initializes the command
func (c *UpgradeCommand) Init(args []string) error {
	if err := c.initConfig(); err != nil {
		return err
	}
	if err := c.initOstree(); err != nil {
		return err
	}

	c.fs.Usage = func() {
		fmt.Printf("Usage: vector %s [options]\n", c.Name())
		c.fs.PrintDefaults()
	}
	return c.fs.Parse(args)
}

// Run runs the command
func (c *UpgradeCommand) Run() error {

	// Check if we are running as root. If running as user, exit with error.
	if getEuid() != 0 {
		return fmt.Errorf("this command must be run as root")
	}

	root, err := c.ot.Root()
	if err != nil {
		return fmt.Errorf("failed to get ostree root: %w", err)
	}

	oldCommit, ref, err := c.getCurrentState()
	if err != nil {
		return fmt.Errorf("failed to get current state: %w", err)
	}

	fmt.Printf("Creating diff for ref: %s\n", ref)
	fmt.Printf("Currently booted commit:  %s\n", oldCommit)

	fmt.Printf("%sFetching updates...%s\n", cBold, cReset)
	if err := c.upgradePull(); err != nil {
		return fmt.Errorf("failed to fetch updates: %w", err)
	}

	newCommit, err := c.ot.LastCommitWithRoot(ref, false)
	if err != nil {
		return fmt.Errorf("failed to get new commit: %w", err)
	}

	updateBootloader := func() error {
		if c.updBootloader {
			if err := c.updateBootloader(newCommit); err != nil {
				return fmt.Errorf("failed to update bootloader: %w", err)
			}
		}
		return nil
	}

	if oldCommit == newCommit {
		fmt.Println("✔ System is already up to date.")
		return updateBootloader()
	}

	fmt.Printf("Available Update: %s\n", newCommit)
	fmt.Println("---------------------------------------------------")

	fmt.Printf("%sAnalyzing package changes...%s\n", cBold, cReset)
	if err := c.analyzeDiff(root, oldCommit, newCommit); err != nil {
		fmt.Printf("Warning: failed to analyze diff: %v\n", err)
	}

	if !c.assumeYes {
		if !c.promptUser("Do you want to apply this upgrade? [y/N] ") {
			fmt.Println("Aborted.")
			return nil
		}
	}

	fmt.Printf("%sDeploying update...%s\n", cBold, cReset)
	if err := c.upgradeDeploy(); err != nil {
		return fmt.Errorf("failed to deploy update: %w", err)
	}

	if err := updateBootloader(); err != nil {
		return err
	}

	fmt.Println("Upgrade successful.")

	fmt.Println("Please reboot at your earliest convenience.")
	return nil
}

func (c *UpgradeCommand) getCurrentState() (string, string, error) {
	deployments, err := c.ot.ListDeployments(false)
	if err != nil {
		return "", "", fmt.Errorf("failed to list deployments: %w", err)
	}

	for _, dep := range deployments {
		if dep.Booted {
			return dep.Checksum, dep.Refspec, nil
		}
	}

	return "", "", fmt.Errorf("no booted deployment found")
}

func (c *UpgradeCommand) upgradePull() error {
	return c.ot.Upgrade([]string{"--pull-only"}, false)
}

func (c *UpgradeCommand) upgradeDeploy() error {
	return c.ot.Upgrade([]string{"--deploy-only"}, false)
}

func (c *UpgradeCommand) updateBootloader(commit string) error {
	fmt.Printf("%sUpdating bootloader binaries...%s\n", cBold, cReset)

	if err := c.updateGrub_x64(commit); err != nil {
		return fmt.Errorf("failed to update GRUB: %w", err)
	}

	// This is a placeholder for the actual bootloader update logic.
	// In a real implementation, this would involve copying files from the new commit to /boot or /efi.
	fmt.Printf("Bootloader binaries updated successfully for commit %s.\n", commit)
	return nil
}

func (c *UpgradeCommand) updateGrub_x64(commit string) error {
	// Search for GRUBX64.EFI files in /efi.
	efiRoot, err := c.cfg.GetItem("Imager.EfiRoot")
	if err != nil {
		return fmt.Errorf("failed to get EfiRoot from config: %w", err)
	}
	if efiRoot == "" {
		return fmt.Errorf("Imager.EfiRoot is not configured in matrixos.conf")
	}
	efiStat, err := os.Stat(efiRoot)
	if err != nil {
		return fmt.Errorf("failed to stat Imager.EfiRoot path: %w", err)
	}
	if !efiStat.IsDir() {
		return fmt.Errorf("Imager.EfiRoot path is not a directory: %s", efiRoot)
	}

	sbCertFileName, err := c.cfg.GetItem("Imager.EfiCertificateFileName")
	if err != nil {
		return fmt.Errorf("failed to get EfiCertificateFileName from config: %w", err)
	}
	if sbCertFileName == "" {
		return fmt.Errorf("Imager.EfiCertificateFileName is not configured in matrixos.conf")
	}

	sbCertPath := filepath.Join(efiRoot, sbCertFileName)
	if _, err := os.Stat(sbCertPath); os.IsNotExist(err) {
		return fmt.Errorf("SecureBoot certificate file not found at expected path: %s", sbCertPath)
	} else if err != nil {
		return fmt.Errorf("failed to stat SecureBoot certificate file: %w", err)
	}

	// use WalkDir to find all GRUBX64.EFI files, then use sbverify.
	efis := []string{}
	err = filepath.WalkDir(efiRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		fname := d.Name()
		if slices.Contains(bootloaders, fname) {
			fmt.Printf("Found EFI file: %s\n", path)

			cmd := execCommand("sbverify", "--cert", sbCertPath, path)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			if err := cmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "Error verifying EFI file %s: %v\n", path, err)
				return nil
			}
			fmt.Printf("Verified EFI file: %s\n", path)
			efis = append(efis, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to collect bootloaders: %w", err)
	}
	for _, efi := range efis {
		efiDir := filepath.Dir(efi)
		fmt.Printf("Updating bootloader binaries in %s...\n", efiDir)
		if err := c.updateGrubDir_x64(efiDir, commit); err != nil {
			return fmt.Errorf("failed to update bootloader binaries: %w", err)
		}
		fmt.Printf("✔ Bootloader binaries updated successfully in %s.\n", efiDir)
	}
	return nil
}

func (c *UpgradeCommand) updateGrubDir_x64(efiDir, commit string) error {
	fmt.Printf("Updating GRUB/Shim in %s for commit %s...\n", efiDir, commit)
	root, err := c.ot.Root()
	if err != nil {
		return fmt.Errorf("failed to get ostree root: %w", err)
	}

	deployments, err := c.ot.ListDeployments(false)
	if err != nil {
		return fmt.Errorf("failed to list deployments: %w", err)
	}

	var foundDep *cds.Deployment
	for _, dep := range deployments {
		if dep.Checksum == commit {
			foundDep = &dep
			break
		}
	}

	if foundDep == nil {
		return fmt.Errorf("deployment not found for commit %s", commit)
	}

	// we do not check other fields because we assume that the upgrade
	// part went fine.

	newRoot := cds.BuildDeploymentRootfs(
		root, foundDep.Stateroot, commit, foundDep.Index,
	)

	filesToCopy := [][2]string{
		{
			filepath.Join(newRoot, "/usr/lib/grub/grub-x86_64.efi.signed"),
			filepath.Join(efiDir, grubEfiBinary),
		},
	}
	// generate /usr/share/shim copy entries.
	shimDir := filepath.Join(newRoot, "/usr/share/shim")
	shimFiles, err := os.ReadDir(shimDir)
	if err == nil {
		for _, entry := range shimFiles {
			if entry.IsDir() {
				continue
			}
			if !entry.Type().IsRegular() {
				continue
			}
			srcPath := filepath.Join(shimDir, entry.Name())
			dstPath := filepath.Join(efiDir, entry.Name())
			filesToCopy = append(filesToCopy, [2]string{srcPath, dstPath})
		}
	} else {
		fmt.Fprintf(os.Stderr, "Warning: failed to read %s directory for new commit: %v\n", shimDir, err)
	}

	for _, pair := range filesToCopy {
		src, dst := pair[0], pair[1]
		if _, err := os.Stat(src); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Expected file was not found in new commit: %s\n", src)
			return nil
		} else if err != nil {
			return fmt.Errorf("failed to stat expected file: %w", err)
		}

		fmt.Printf("Copying %s to %s...\n", src, dst)
		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("failed to copy file: %w", err)
		}
	}

	return nil
}

func (c *UpgradeCommand) promptUser(prompt string) bool {
	fmt.Print(prompt)
	var response string
	fmt.Scanln(&response)
	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes"
}

func (c *UpgradeCommand) analyzeDiff(root, oldSHA, newSHA string) error {
	oldPkgs, err := c.listPackages(root, oldSHA)
	if err != nil {
		return err
	}
	newPkgs, err := c.listPackages(root, newSHA)
	if err != nil {
		return err
	}

	removed := make(map[string]bool)
	added := make(map[string]bool)

	for pkg := range oldPkgs {
		if !newPkgs[pkg] {
			removed[pkg] = true
		}
	}
	for pkg := range newPkgs {
		if !oldPkgs[pkg] {
			added[pkg] = true
		}
	}

	if len(removed) == 0 && len(added) == 0 {
		fmt.Println("No package changes detected (Config/Binary only update).")
		return nil
	}

	fmt.Printf("%sPackage Changes:%s\n", cBold, cReset)

	var removedList []string
	for pkg := range removed {
		removedList = append(removedList, pkg)
	}
	sort.Strings(removedList)

	for _, pkg := range removedList {
		baseName := c.getPackageBaseName(pkg)
		var newVer string
		for addedPkg := range added {
			if c.getPackageBaseName(addedPkg) == baseName {
				newVer = addedPkg
				break
			}
		}

		if newVer != "" {
			fmt.Printf("  ✔ %s%s%s -> %s%s%s\n", cYellow, pkg, cReset, cGreen, newVer, cReset)
			delete(added, newVer)
		} else {
			fmt.Printf("  ❌ %s%s%s (Removed)\n", cRed, pkg, cReset)
		}
	}

	var addedList []string
	for pkg := range added {
		addedList = append(addedList, pkg)
	}
	sort.Strings(addedList)

	for _, pkg := range addedList {
		fmt.Printf("  ✨ %s%s%s (New)\n", cGreen, pkg, cReset)
	}

	fmt.Println("---------------------------------------------------")
	return nil
}

func (c *UpgradeCommand) listPackages(root, commit string) (map[string]bool, error) {
	pkgs, err := c.listPackagesFromPath(root, commit, "/usr/var-db-pkg")
	if err == nil && len(pkgs) > 0 {
		return pkgs, nil
	}
	return c.listPackagesFromPath(root, commit, "/var/db/pkg")
}

func (c *UpgradeCommand) listPackagesFromPath(root, commit, path string) (map[string]bool, error) {
	cmd := execCommand("ostree", getRepoFlag(root), "ls", "-R", commit, "--", path)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	pkgs := make(map[string]bool)
	scanner := bufio.NewScanner(bytes.NewReader(out))

	prefix := path
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 5 {
			continue
		}

		mode := parts[0]
		fpath := parts[4]

		if !strings.HasPrefix(mode, "d") {
			continue
		}
		if !strings.HasPrefix(fpath, prefix) {
			continue
		}

		relPath := strings.TrimPrefix(fpath, prefix)
		relPath = strings.TrimSuffix(relPath, "/")

		if strings.Count(relPath, "/") == 1 {
			pkgs[relPath] = true
		}
	}
	return pkgs, nil
}

func (c *UpgradeCommand) getPackageBaseName(pkg string) string {
	parts := strings.SplitN(pkg, "/", 2)
	if len(parts) != 2 {
		return pkg
	}
	category := parts[0]
	rest := parts[1]

	lastHyphen := -1
	for i := len(rest) - 1; i >= 0; i-- {
		if rest[i] == '-' {
			if i+1 < len(rest) && unicode.IsDigit(rune(rest[i+1])) {
				lastHyphen = i
				break
			}
		}
	}

	if lastHyphen != -1 {
		name := rest[:lastHyphen]
		return category + "/" + name
	}
	return pkg
}
