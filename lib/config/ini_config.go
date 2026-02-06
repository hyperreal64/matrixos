package config

import (
	"fmt"
	"os"
	"path/filepath"
)

type searchPath struct {
	fileName    string
	dirPath     string
	defaultRoot string
}

const (
	configFileName = "matrixos.conf"
)

var (
	searchPaths = []searchPath{
		// Setup for when vector runs from the git repo.
		{
			fileName:    configFileName,
			dirPath:     "./conf",
			defaultRoot: ".",
		},
		// Setup for when vector runs from its base directory.
		{
			fileName:    configFileName,
			dirPath:     "../conf",
			defaultRoot: "..",
		},
		// Setup for when vector runs from an installed location,
		// with config in /etc/matrixos/conf.
		{
			fileName:    configFileName,
			dirPath:     "/etc/matrixos/conf",
			defaultRoot: "/var/lib/matrixos",
		},
	}
)

// IniConfig is a config reader that loads values from an INI file.
type IniConfig struct {
	sp  *searchPath
	cfg map[string][]string
}

func cfgPathToSearchPath(fullPath string) *searchPath {
	for _, sp := range searchPaths {
		searchFullPath := filepath.Join(sp.dirPath, sp.fileName)
		if fullPath != searchFullPath && fullPath != "" {
			continue
		}
		if _, err := filepath.Abs(searchFullPath); err != nil {
			continue
		}
		if _, err := os.Stat(searchFullPath); err == nil {
			return &sp
		}
	}
	return nil
}

// smartRootify translates matrixOS.Root into a path that's complying with the config var
// specifications.
func smartRootify(path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}

	// Get the working directory so that we can compare it with path.
	wd, err := os.Getwd()
	if err != nil {
		return wd, err
	}

	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return pathAbs, err
	}
	cwdAbs, err := filepath.Abs(wd)
	if err != nil {
		return cwdAbs, err
	}

	if pathAbs == cwdAbs {
		// This means that matrixOS.Root is set to ./ or .
		// Which means that we need to make this ../ as vector is in one subdir deeper.
		return filepath.Abs("..")
	}
	return pathAbs, nil
}

func NewIniConfig() (IConfig, error) {
	sp := cfgPathToSearchPath("")
	if sp == nil {
		return nil, fmt.Errorf("config file not found in search paths: %v", searchPaths)
	}

	fullPath := filepath.Join(sp.dirPath, sp.fileName)
	return NewIniConfigFromFile(fullPath, sp.defaultRoot)
}

// NewIniConfig creates a new IniConfig instance with the specified file path.
func NewIniConfigFromFile(path string, defaultRoot string) (IConfig, error) {
	sp := searchPath{
		fileName:    filepath.Base(path),
		dirPath:     filepath.Dir(path),
		defaultRoot: defaultRoot,
	}
	return &IniConfig{
		sp: &sp,
	}, nil
}

func (c *IniConfig) Load() error {
	if c == nil {
		return fmt.Errorf("config is nil")
	}
	if c.sp == nil {
		return fmt.Errorf(
			"Unable to find %v config file in search paths: %v",
			configFileName,
			searchPaths,
		)
	}
	fullPath := filepath.Join(c.sp.dirPath, c.sp.fileName)
	ini, err := LoadConfig(fullPath)
	if err != nil {
		return err
	}

	c.cfg = make(map[string][]string)
	for section, items := range ini {
		for key, value := range items {
			// Flatten the key: [Section] Key -> Section.Key
			var fullKey string
			if section == "" {
				fullKey = key
			} else {
				fullKey = fmt.Sprintf("%s.%s", section, key)
			}
			c.cfg[fullKey] = []string{value}
		}
	}

	// Set defaults for base paths if missing, to allow expansion
	rootVal, foundRoot := c.getVal("matrixOS.Root")
	if !foundRoot {
		c.setVal("matrixOS.Root", c.sp.defaultRoot)
	} else {
		rootVal, err := smartRootify(rootVal)
		if err != nil {
			return err
		}
		c.setVal("matrixOS.Root", rootVal)
	}

	// Expand base paths to absolute
	if err := c.expandAbs("matrixOS.Root"); err != nil {
		return err
	}
	// PrivateGitRepoPath is usually absolute, but if relative, treat as relative to CWD
	if _, ok := c.getVal("matrixOS.PrivateGitRepoPath"); ok {
		if err := c.expandAbs("matrixOS.PrivateGitRepoPath"); err != nil {
			return err
		}
	}

	rootDependents := []string{
		"matrixOS.PrivateGitRepoPath",
		"matrixOS.LogsDir",
		"matrixOS.LocksDir",
		"Seeder.DownloadsDir",
		"Seeder.DistfilesDir",
		"Seeder.BinpkgsDir",
		"Seeder.PortageReposDir",
		"Seeder.GpgKeysDir",
		"Imager.ImagesDir",
		"Ostree.RepoDir",
		"Ostree.DevGpgHomeDir",
		"Ostree.GpgOfficialPublicKey",
	}
	for _, key := range rootDependents {
		c.expand(key, "matrixOS.Root")
	}

	privateRepoDependents := []string{
		"Seeder.SecureBootPrivateKey",
		"Seeder.SecureBootPublicKey",
		"Ostree.GpgPrivateKey",
		"Ostree.GpgPublicKey",
	}
	for _, key := range privateRepoDependents {
		c.expand(key, "matrixOS.PrivateGitRepoPath")
	}

	return nil
}

func (c *IniConfig) GetItem(key string) (SingleConfigValue, error) {
	cfg := SingleConfigValue{}
	if c == nil {
		return cfg, fmt.Errorf("config is nil")
	}

	lst, ok := c.cfg[key]
	if !ok {
		return cfg, fmt.Errorf("invalid key %s", key)
	}
	if len(lst) > 0 {
		cfg.Item = lst[0]
	}
	return cfg, nil
}

func (c *IniConfig) GetItems(key string) (MultipleConfigValues, error) {
	cfg := MultipleConfigValues{}
	if c == nil {
		return cfg, fmt.Errorf("config is nil")
	}

	lst, ok := c.cfg[key]
	if !ok {
		return cfg, fmt.Errorf("invalid key %s", key)
	}
	cfg.Items = lst
	return cfg, nil
}

func (c *IniConfig) getVal(key string) (string, bool) {
	if vals, ok := c.cfg[key]; ok && len(vals) > 0 {
		return vals[0], true
	}
	return "", false
}

func (c *IniConfig) setVal(key, val string) {
	c.cfg[key] = []string{val}
}

func (c *IniConfig) expandAbs(key string) error {
	val, ok := c.getVal(key)
	if !ok {
		return nil
	}
	if !filepath.IsAbs(val) {
		abs, err := filepath.Abs(val)
		if err != nil {
			return fmt.Errorf("failed to resolve absolute path for %s: %w", key, err)
		}
		c.setVal(key, abs)
	}
	return nil
}

func (c *IniConfig) expand(key, baseKey string) {
	val, ok := c.getVal(key)
	if !ok {
		return
	}
	if filepath.IsAbs(val) {
		return
	}
	base, ok := c.getVal(baseKey)
	if !ok {
		return
	}
	c.setVal(key, filepath.Join(base, val))
}
