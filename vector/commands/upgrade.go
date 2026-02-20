package commands

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode"

	"matrixos/vector/lib/cds"
)

var (
	grubEfiBinary = "GRUBX64.EFI"
	bootloaders   = []string{
		grubEfiBinary,
	}
)

// UpgradeCommand is a command for upgrading the system
type UpgradeCommand struct {
	BaseCommand
	UI
	fs            *flag.FlagSet
	assumeYes     bool
	updBootloader bool
	pretend       bool
	verbose       bool
	force         bool
}

// NewUpgradeCommand creates a new UpgradeCommand
func NewUpgradeCommand() ICommand {
	return &UpgradeCommand{}
}

// Name returns the name of the command
func (c *UpgradeCommand) Name() string {
	return "upgrade"
}

// Init initializes the command
func (c *UpgradeCommand) Init(args []string) error {
	if err := c.initConfig(); err != nil {
		return err
	}

	if err := c.initOstree(); err != nil {
		return err
	}

	c.StartUI()

	return c.parseArgs(args)
}

// parseArgs parses the command-line arguments without initializing config or ostree.
func (c *UpgradeCommand) parseArgs(args []string) error {
	c.fs = flag.NewFlagSet("upgrade", flag.ContinueOnError)
	c.fs.BoolVar(&c.updBootloader, "update-bootloader", false,
		"Update bootloader binaries in /efi")
	c.fs.BoolVar(&c.assumeYes, "y", false, "Assume yes to all prompts")
	c.fs.BoolVar(&c.pretend, "pretend", false, "Only fetch updates and show diff without applying them")
	c.fs.BoolVar(&c.verbose, "verbose", false, "Show detailed output")
	c.fs.BoolVar(&c.force, "force", false, "Force upgrade even if up to date")
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

	oldCommit, ref, err := c.getCurrentState()
	if err != nil {
		return fmt.Errorf("failed to get current state: %w", err)
	}

	fmt.Printf("%s%sChecking for updates on branch: %s%s\n",
		c.cBlue, c.iconSearch, ref, c.cReset)
	fmt.Printf("   %sCurrent version: %s%s\n", c.cBold, oldCommit, c.cReset)

	fmt.Printf("\n%s%sFetching updates...%s\n",
		c.cBold, c.iconDownload, c.cReset)
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
		fmt.Printf("\n%s%sSystem is already up to date.%s\n",
			c.cGreen, c.iconCheck, c.cReset)
		if !c.force {
			return updateBootloader()
		}
		fmt.Printf("\n%s%sForcing update despite no changes...%s\n",
			c.cYellow, c.iconWarn, c.cReset)
	} else {
		fmt.Printf("\n%s%sUpdate Available: %s%s\n",
			c.cGreen, c.iconNew, newCommit, c.cReset)
	}
	fmt.Println(c.separator)

	fmt.Printf("\n%s%sAnalyzing package changes...%s\n",
		c.cBold, c.iconPackage, c.cReset)
	if err := c.analyzeDiff(oldCommit, newCommit); err != nil {
		fmt.Printf("Warning: failed to analyze diff: %v\n", err)
	}
	if err := c.analyzeEtcChanges(oldCommit, newCommit); err != nil {
		fmt.Printf("Warning: failed to analyze /etc changes: %v\n", err)
	}
	fmt.Println(c.separator)

	if c.pretend {
		fmt.Printf("\n%sRunning in pretend mode. Exiting.%s\n", c.cYellow, c.cReset)
		return nil
	}

	if !c.assumeYes {
		fmt.Println("")
		promptMsg := fmt.Sprintf(
			"%s%sDo you want to apply this upgrade? [y/N] %s",
			c.cYellow, c.iconQuestion, c.cReset,
		)
		if !c.promptUser(promptMsg) {
			fmt.Printf("%sAborted.%s\n", c.iconError, c.cReset)
			return nil
		}
	}

	fmt.Printf("\n%s%sDeploying update...%s\n", c.cBold, c.iconRocket, c.cReset)
	if err := c.upgradeDeploy(); err != nil {
		return fmt.Errorf("failed to deploy update: %w", err)
	}

	if err := updateBootloader(); err != nil {
		return err
	}

	fmt.Printf("\n%s%sUpgrade successful!%s\n", c.cGreen, c.iconCheck, c.cReset)

	fmt.Printf("%s%sPlease reboot at your earliest convenience.%s\n",
		c.cYellow, c.iconWarn, c.cReset)
	return nil
}

func (c *UpgradeCommand) getCurrentState() (string, string, error) {
	deployments, err := c.ot.ListRootDeployments(false)
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
	fmt.Printf("\n%s%sUpdating bootloader binaries...%s\n",
		c.cBold, c.iconGear, c.cReset)

	if err := c.updateGrub_x64(commit); err != nil {
		return fmt.Errorf("failed to update GRUB: %w", err)
	}

	// This is a placeholder for the actual bootloader update logic.
	// In a real implementation, this would involve copying files from the new
	// commit to /boot or /efi.
	fmt.Printf("%s%sBootloader updated successfully for commit %s.%s\n",
		c.cGreen, c.iconCheck, commit, c.cReset)
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
		return fmt.Errorf("certificate file not found at: %s", sbCertPath)
	} else if err != nil {
		return fmt.Errorf("failed to stat SecureBoot certificate file: %w", err)
	}

	// use WalkDir to find all GRUBX64.EFI files, then use sbverify.
	efis := []string{}
	err = filepath.WalkDir(efiRoot, func(
		path string, d os.DirEntry, err error,
	) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		fname := d.Name()
		if slices.Contains(bootloaders, fname) {
			fmt.Printf("   Found EFI file: %s%s%s\n", c.cBlue, path, c.cReset)

			cmd := execCommand("sbverify", "--cert", sbCertPath, path)
			// cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			if err := cmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr,
					"   %s%sError verifying EFI file %s: %v%s\n",
					c.cRed, c.iconError, path, err, c.cReset)
				return nil
			}
			fmt.Printf("   %sVerified EFI file: %s%s%s\n",
				c.iconCheck, c.cGreen, path, c.cReset)
			efis = append(efis, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to collect bootloaders: %w", err)
	}
	for _, efi := range efis {
		efiDir := filepath.Dir(efi)
		fmt.Printf("   %sUpdating bootloader binaries in %s...\n",
			c.iconPackage, efiDir)
		if err := c.updateGrubDir_x64(efiDir, commit); err != nil {
			return fmt.Errorf("failed to update bootloader binaries: %w", err)
		}
		fmt.Printf("   %sBootloader binaries updated successfully in %s.\n",
			c.iconCheck, efiDir)
	}
	return nil
}

func (c *UpgradeCommand) updateGrubDir_x64(efiDir, commit string) error {
	fmt.Printf(
		"   %sUpdating GRUB/Shim in %s%s%s for commit %s%s%s...\n",
		c.iconUpdate, c.cBlue, efiDir, c.cReset, c.cBold, commit, c.cReset,
	)
	root, err := c.ot.Root()
	if err != nil {
		return fmt.Errorf("failed to get ostree root: %w", err)
	}

	deployments, err := c.ot.ListRootDeployments(false)
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
		fmt.Fprintf(os.Stderr,
			"%s%sWarning: failed to read %s directory for new commit: %v%s\n",
			c.cYellow, c.iconWarn, shimDir, err, c.cReset)
	}

	for _, pair := range filesToCopy {
		src, dst := pair[0], pair[1]
		if _, err := os.Stat(src); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr,
				"%s%sExpected file was not found in new commit: %s%s\n",
				c.cYellow, c.iconWarn, src, c.cReset)
			return nil
		} else if err != nil {
			return fmt.Errorf("failed to stat expected file: %w", err)
		}

		fmt.Printf("   %sCopying %s to %s%s%s...\n",
			c.iconDoc, filepath.Base(src), c.cBold, dst, c.cReset)
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

func (c *UpgradeCommand) analyzeDiff(oldSHA, newSHA string) error {
	opkgs, err := c.ot.ListPackages(oldSHA, false)
	if err != nil {
		return err
	}
	oldPkgs := make(map[string]bool)
	for _, pkg := range opkgs {
		oldPkgs[pkg] = true
	}

	npkgs, err := c.ot.ListPackages(newSHA, false)
	if err != nil {
		return err
	}
	newPkgs := make(map[string]bool)
	for _, pkg := range npkgs {
		newPkgs[pkg] = true
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
		fmt.Printf(
			"   %s%sNo package changes detected (Config/Binary only update).%s\n",
			c.cBlue, c.iconPackage, c.cReset,
		)
		return nil
	}

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
			fmt.Printf("   %s %s%s%s -> %s%s%s\n",
				c.iconUpdate, c.cYellow, pkg, c.cReset,
				c.cGreen, newVer, c.cReset)
			delete(added, newVer)
		} else {
			fmt.Printf("   %s %s%s%s (Removed)\n",
				c.iconError, c.cRed, pkg, c.cReset)
		}
	}

	var addedList []string
	for pkg := range added {
		addedList = append(addedList, pkg)
	}
	sort.Strings(addedList)

	for _, pkg := range addedList {
		fmt.Printf("   %s %s%s%s (New)\n",
			c.iconNew, c.cGreen, pkg, c.cReset)
	}

	fmt.Println(c.separator)
	return nil
}

func (c *UpgradeCommand) analyzeEtcChanges(oldSHA, newSHA string) error {
	changes, err := c.ot.ListEtcChanges(oldSHA, newSHA)
	if err != nil {
		return err
	}

	if len(changes) == 0 {
		fmt.Printf(
			"   %s%sNo /etc changes detected (Config/Binary only update).%s\n",
			c.cBlue, c.iconPackage, c.cReset,
		)
		return nil
	}
	fmt.Printf("   %s%s/etc changes detected:%s\n", c.cYellow, c.iconPackage, c.cReset)

	output := c.formatEtcChanges(changes)

	// Use a pager if the output is large (more than 30 lines)
	lines := strings.Count(output, "\n")
	if lines > 30 && !c.assumeYes {
		return c.showWithPager(output)
	}
	fmt.Print(output)
	return nil
}

// pagerBinary is the pager command to use for long output.
var pagerBinary = "less"

// formatEtcChanges renders the list of EtcChange entries into a
// human-readable string using the UI icons and colours.
func (c *UpgradeCommand) formatEtcChanges(changes []cds.EtcChange) string {
	var b strings.Builder

	// Group changes by action for a structured summary.
	var conflicts, updates, adds, removes, userOnly []cds.EtcChange
	for _, ch := range changes {
		switch ch.Action {
		case cds.EtcActionConflict:
			conflicts = append(conflicts, ch)
		case cds.EtcActionUpdate:
			updates = append(updates, ch)
		case cds.EtcActionAdd:
			adds = append(adds, ch)
		case cds.EtcActionRemove:
			removes = append(removes, ch)
		case cds.EtcActionUserOnly:
			userOnly = append(userOnly, ch)
		}
	}

	somethingPrinted := false

	// Conflicts first — they require attention.
	if len(conflicts) > 0 {
		somethingPrinted = true
		fmt.Fprintf(&b, "\n   %s%s Conflicts (manual resolution required):%s\n",
			c.cRed, c.iconWarn, c.cReset)
		for _, ch := range conflicts {
			fmt.Fprintf(&b, "      %s %s/etc/%s%s\n",
				c.iconError, c.cRed, ch.Path, c.cReset)
			c.writeChangeDetail(&b, ch)
		}
	}

	// Updates — clean upstream changes that will be applied.
	if len(updates) > 0 {
		somethingPrinted = true
		fmt.Fprintf(&b, "\n   %s%s Updated by upstream (will be applied):%s\n",
			c.cGreen, c.iconUpdate, c.cReset)
		for _, ch := range updates {
			fmt.Fprintf(&b, "      %s %s/etc/%s%s\n",
				c.iconUpdate, c.cGreen, ch.Path, c.cReset)
			c.writeChangeDetail(&b, ch)
		}
	}

	// Adds — new files from upstream.
	if len(adds) > 0 {
		somethingPrinted = true
		fmt.Fprintf(&b, "\n   %s%s New files from upstream:%s\n",
			c.cGreen, c.iconNew, c.cReset)
		for _, ch := range adds {
			fmt.Fprintf(&b, "      %s %s/etc/%s%s\n",
				c.iconNew, c.cGreen, ch.Path, c.cReset)
			c.writeChangeDetail(&b, ch)
		}
	}

	// Removes — files removed upstream.
	if len(removes) > 0 {
		somethingPrinted = true
		fmt.Fprintf(&b, "\n   %s%s Removed by upstream (will be deleted):%s\n",
			c.cYellow, c.iconError, c.cReset)
		for _, ch := range removes {
			fmt.Fprintf(&b, "      %s %s/etc/%s%s\n",
				c.iconError, c.cYellow, ch.Path, c.cReset)
		}
	}

	// User-only — local changes preserved as-is.
	if len(userOnly) > 0 && c.verbose {
		somethingPrinted = true
		fmt.Fprintf(&b, "\n   %s%s User modifications (preserved):%s\n",
			c.cBlue, c.iconDoc, c.cReset)
		for _, ch := range userOnly {
			fmt.Fprintf(&b, "      %s %s/etc/%s%s\n",
				c.iconDoc, c.cBlue, ch.Path, c.cReset)
		}
	}

	if !somethingPrinted {
		fmt.Fprintf(&b, "\n   %s%s No changes worth highlighting.%s\n",
			c.cBlue, c.iconPackage, c.cReset)
	}

	// Summary line
	fmt.Fprintf(&b, "\n   %sSummary:%s %d conflict(s), %d update(s), %d add(s), %d remove(s), %d user-only\n",
		c.cBold, c.cReset,
		len(conflicts), len(updates), len(adds), len(removes), len(userOnly))

	return b.String()
}

// writeChangeDetail appends detail lines about what changed for a path.
func (c *UpgradeCommand) writeChangeDetail(b *strings.Builder, ch cds.EtcChange) {
	if ch.Old != nil && ch.New != nil {
		oldDesc := ch.Old.String()
		newDesc := ch.New.String()
		if oldDesc != newDesc {
			fmt.Fprintf(b, "        %swas:%s %s\n", c.cBold, c.cReset, oldDesc)
			fmt.Fprintf(b, "        %snow:%s %s\n", c.cBold, c.cReset, newDesc)
		}
	} else if ch.New != nil {
		fmt.Fprintf(b, "        %snew:%s %s\n", c.cBold, c.cReset, ch.New.String())
	}
	if ch.User != nil && ch.Old != nil && !ch.User.Equals(ch.Old) {
		fmt.Fprintf(b, "        %slocal:%s %s\n", c.cBold, c.cReset, ch.User.String())
	}
}

// showWithPager pipes the given text through a pager (e.g. less).
func (c *UpgradeCommand) showWithPager(text string) error {
	pager := os.Getenv("PAGER")
	if pager == "" {
		pager = pagerBinary
	}
	cmd := execCommand(pager, "-R")
	cmd.Stdin = strings.NewReader(text)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
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
