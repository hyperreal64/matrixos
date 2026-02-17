package commands

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"os"
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

// UpgradeCommand is a command for upgrading the system
type UpgradeCommand struct {
	fs        *flag.FlagSet
	cfg       config.IConfig
	ot        *cds.Ostree
	reboot    bool
	assumeYes bool
}

// NewUpgradeCommand creates a new UpgradeCommand
func NewUpgradeCommand() ICommand {
	c := &UpgradeCommand{
		fs: flag.NewFlagSet("upgrade", flag.ExitOnError),
	}
	c.fs.BoolVar(&c.reboot, "reboot", false, "Reboot after successful upgrade")
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

	oldSHA, ref, err := c.getCurrentState()
	if err != nil {
		return fmt.Errorf("failed to get current state: %w", err)
	}

	fmt.Printf("Creating diff for ref: %s\n", ref)
	fmt.Printf("Current Booted SHA:  %s\n", oldSHA)

	fmt.Printf("%sFetching updates...%s\n", cBold, cReset)
	if err := c.upgradePull(); err != nil {
		return fmt.Errorf("failed to fetch updates: %w", err)
	}

	newSHA, err := c.ot.LastCommitWithRoot(ref, false)
	if err != nil {
		return fmt.Errorf("failed to get new commit SHA: %w", err)
	}

	if oldSHA == newSHA {
		fmt.Println("✅ System is already up to date.")
		return nil
	}

	fmt.Printf("Available Update SHA: %s\n", newSHA)
	fmt.Println("---------------------------------------------------")

	fmt.Printf("%sAnalyzing package changes...%s\n", cBold, cReset)
	if err := c.analyzeDiff(root, oldSHA, newSHA); err != nil {
		fmt.Printf("Warning: failed to analyze diff: %v\n", err)
	}

	if !c.assumeYes {
		if !c.promptUser("Do you want to apply this upgrade? [y/N] ") {
			fmt.Println("Aborted.")
			return nil
		}
	}

	fmt.Printf("%sDeploying update...%s\n", cBold, cReset)
	if err := c.runCommand("ostree", "admin", "upgrade", "--deploy-only"); err != nil {
		return fmt.Errorf("failed to deploy update: %w", err)
	}

	fmt.Println("Upgrade successful.")

	if c.reboot {
		fmt.Println("Rebooting...")
		return c.runCommand("reboot")
	}

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

func (c *UpgradeCommand) readOriginRefspec(path string) (string, error) {
	cfg, err := config.LoadConfig(path)
	if err != nil {
		return "", fmt.Errorf("failed to load origin file: %w", err)
	}

	if section, ok := cfg["origin"]; ok {
		if refspec, ok := section["refspec"]; ok {
			return refspec, nil
		}
	}

	return "", fmt.Errorf("refspec not found in [origin] section")
}

func (c *UpgradeCommand) upgradePull() error {
	return c.ot.Upgrade([]string{"--pull-only"}, false)
}

func (c *UpgradeCommand) runCommand(name string, args ...string) error {
	cmd := execCommand(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
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
