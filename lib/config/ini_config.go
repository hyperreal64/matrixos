package config

import (
	"fmt"
	"path/filepath"
)

// IniConfig is a config reader that loads values from an INI file.
type IniConfig struct {
	Path string
	cfg  map[string][]string
}

// NewIniConfig creates a new IniConfig instance with the specified file path.
func NewIniConfig(path string) *IniConfig {
	return &IniConfig{
		Path: path,
	}
}

func (c *IniConfig) Load() error {
	ini, err := LoadConfig(c.Path)
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
	if _, ok := c.getVal("matrixOS.Root"); !ok {
		c.setVal("matrixOS.Root", ".")
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

	// Level 1: Relative to Root
	rootDependents := []string{
		"matrixOS.ArtifactsDir",
		"matrixOS.LogsDir",
		"matrixOS.LocksDir",
		"Ostree.Dir",
		"Ostree.GpgOfficialPublicKey",
	}
	for _, key := range rootDependents {
		c.expand(key, "matrixOS.Root")
	}

	// Level 2: Relative to ArtifactsDir
	artifactsDependents := []string{
		"matrixOS.OutDir",
		"Ostree.DevGpgHomeDir",
	}
	for _, key := range artifactsDependents {
		c.expand(key, "matrixOS.ArtifactsDir")
	}

	// Level 3: Relative to OutDir
	outDependents := []string{
		"Seeder.OutDir",
		"Imager.OutDir",
	}
	for _, key := range outDependents {
		c.expand(key, "matrixOS.OutDir")
	}

	// Level 4: Relative to Seeder.OutDir
	seederDependents := []string{
		"Seeder.DownloadsDir",
		"Seeder.DistfilesDir",
		"Seeder.BinpkgsDir",
		"Seeder.PortageReposDir",
		"Seeder.GpgKeysDir",
	}
	for _, key := range seederDependents {
		c.expand(key, "Seeder.OutDir")
	}

	// Level X: Relative to PrivateGitRepoPath
	privateRepoDependents := []string{
		"Seeder.SecureBootPrivateKey",
		"Seeder.SecureBootPublicKey",
		"Ostree.GpgPrivateKey",
		"Ostree.GpgPublicKey",
	}
	for _, key := range privateRepoDependents {
		c.expand(key, "matrixOS.PrivateGitRepoPath")
	}

	// Level Y: Relative to Ostree.Dir
	c.expand("Ostree.RepoDir", "Ostree.Dir")

	return nil
}

func (c *IniConfig) GetItem(key string) (SingleConfigValue, error) {
	cfg := SingleConfigValue{}
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
