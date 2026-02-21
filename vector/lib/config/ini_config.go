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

// ConfigPath returns the full path to the config file for this search path.
func (sp *searchPath) ConfigPath() string {
	return filepath.Join(sp.dirPath, sp.fileName)
}

const (
	// BaseConfigFileName is the name of the main configuration file that vector looks for.
	BaseConfigFileName = "matrixos.conf"
	// ClientConfigFileName is the name of the client configuration file that vector looks for.
	ClientConfigFileName = "client.conf"
)

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

func searchPaths(cfgName string) []searchPath {
	// Navigate CWD up until we find a .matrixos file.
	var sps []searchPath

	cwd, err := os.Getwd()
	if err != nil {
		return sps
	}

	goUp := func() bool {
		parent := filepath.Dir(cwd)
		if parent == cwd {
			return false
		}
		cwd = parent
		return true
	}

	for {
		dotMatrixosPath := filepath.Join(cwd, ".matrixos")
		if _, err := os.Stat(dotMatrixosPath); err != nil {
			if os.IsNotExist(err) {
				if !goUp() {
					break
				}
				continue
			}
			// Error found, and is not "not exist".
			break
		}

		sps = append(sps, searchPath{
			fileName:    cfgName,
			dirPath:     filepath.Join(cwd, "conf"),
			defaultRoot: cwd,
		})
		parent := filepath.Dir(cwd)
		if parent == cwd {
			break
		}
		cwd = parent
	}

	// add this as last resort option at the moment.
	sps = append(sps, searchPath{
		// Setup for when vector runs from an installed location,
		// with config in /etc/matrixos/conf.
		fileName:    cfgName,
		dirPath:     "/etc/matrixos/conf",
		defaultRoot: "/usr/lib/matrixos",
	})

	return sps
}

// IniConfig is a config reader that loads values from an INI file.
type IniConfig struct {
	parent IConfig
	sp     *searchPath
	cfg    map[string][]string
}

func cfgNameToSearchPath(cfgName string) *searchPath {
	for _, sp := range searchPaths(cfgName) {
		searchFullPath := sp.ConfigPath()
		if _, err := filepath.Abs(searchFullPath); err != nil {
			continue
		}
		if _, err := os.Stat(searchFullPath); err == nil {
			return &sp
		}
	}
	return nil
}

// NewBaseConfig creates a new IniConfig instance for the base configuration.
func NewBaseConfig() (IConfig, error) {
	return NewIniConfig(BaseConfigFileName)
}

// NewClientConfig creates a new IniConfig instance for the client configuration.
func NewClientConfig() (IConfig, error) {
	return NewIniConfig(ClientConfigFileName)
}

// NewIniConfig creates a new IniConfig instance.
func NewIniConfig(configName string) (IConfig, error) {
	sp := cfgNameToSearchPath(configName)
	if sp == nil {
		return nil, fmt.Errorf(
			"config file not found in any of the paths: %v",
			searchPaths(configName),
		)
	}
	return &IniConfig{
		sp: sp,
	}, nil
}

// ConfigFromPathParams holds parameters for creating a config from a specific path.
type ConfigFromPathParams struct {
	ConfigPath  string
	DefaultRoot string
}

// NewIniConfigFromPath creates a new IniConfig instance with the specified file path.
func NewIniConfigFromPath(params *ConfigFromPathParams) (IConfig, error) {
	if params == nil {
		return nil, fmt.Errorf("params is nil")
	}
	if params.ConfigPath == "" {
		return nil, fmt.Errorf("config path is empty")
	}
	if params.DefaultRoot == "" {
		return nil, fmt.Errorf("default root is empty")
	}
	sp := searchPath{
		fileName:    filepath.Base(params.ConfigPath),
		dirPath:     filepath.Dir(params.ConfigPath),
		defaultRoot: params.DefaultRoot,
	}
	return &IniConfig{
		sp: &sp,
	}, nil
}

func (c *IniConfig) loadAndGenerateConfig(configPath string) error {
	ini, err := LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config %s: %w", configPath, err)
	}
	c.generateConfig(ini)
	return nil
}

func (c *IniConfig) generateSubConfigs(configPath string) error {
	// configPath is a valid path to a config file.
	// Use this path to build a list of subconfigs to load, starting
	// with configPath + ".d/*.conf".
	subconfigDir := configPath + ".d"
	subconfigs, err := os.ReadDir(subconfigDir)
	if err != nil {
		// If the directory doesn't exist, that's fine.
		// it just means there are no subconfigs.
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf(
			"failed to read subconfig directory %s: %w",
			subconfigDir,
			err,
		)
	}

	for _, entry := range subconfigs {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".conf" {
			continue
		}
		subconfigPath := filepath.Join(subconfigDir, entry.Name())
		err := c.loadAndGenerateConfig(subconfigPath)
		if err != nil {
			return fmt.Errorf(
				"failed to load subconfig %s: %w",
				subconfigPath,
				err,
			)
		}
	}

	return nil
}

func (c *IniConfig) generateConfig(ini IniFile) {
	if c.cfg == nil {
		c.cfg = make(map[string][]string)
	}

	for section, items := range ini {
		for key, value := range items {
			// Flatten the key: [Section] Key -> Section.Key
			var fullKey string
			if section == "" {
				fullKey = key
			} else {
				fullKey = fmt.Sprintf("%s.%s", section, key)
			}

			val, ok := c.cfg[fullKey]
			if !ok {
				val = []string{}
			}
			val = append(val, value) // preserve history.
			c.cfg[fullKey] = val
		}
	}
}

func (c *IniConfig) generateParent(ini IniFile) error {
	mos, ok := ini["matrixOS"]
	if !ok {
		return nil
	}
	parentVal, ok := mos["ParentConfig"]
	if !ok {
		return nil
	}
	parentPath := filepath.Join(c.sp.dirPath, parentVal)
	if _, err := os.Stat(parentPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	c.loadAndGenerateConfig(parentPath)
	return nil
}

func (c *IniConfig) Load() error {
	if c == nil {
		return fmt.Errorf("config is nil")
	}
	if c.sp == nil {
		return fmt.Errorf("no configuration found in any of the search paths.")
	}
	fullPath := c.sp.ConfigPath()
	ini, err := LoadConfig(fullPath)
	if err != nil {
		return err
	}

	c.generateParent(ini)
	c.generateConfig(ini)
	if err := c.generateSubConfigs(fullPath); err != nil {
		return err
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
		"Seeder.LocksDir",
		"Seeder.DownloadsDir",
		"Seeder.DistfilesDir",
		"Seeder.BinpkgsDir",
		"Seeder.PortageReposDir",
		"Seeder.GpgKeysDir",
		"Releaser.HooksDir",
		"Releaser.LocksDir",
		"Imager.ImagesDir",
		"Imager.LocksDir",
		"Imager.MountDir",
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
	defaultPrivateRepoDependents := []string{
		"Seeder.DefaultSecureBootPrivateKey",
		"Seeder.DefaultSecureBootPublicKey",
	}
	for _, key := range defaultPrivateRepoDependents {
		c.expand(key, "matrixOS.DefaultPrivateGitRepoPath")
	}

	return nil
}

// GetItem retrieves the single config value associated to the provided config key.
// If multiple values are present, it returns the last one.
func (c *IniConfig) GetItem(key string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("config is nil")
	}

	lst, ok := c.cfg[key]
	if !ok {
		return "", fmt.Errorf("invalid key %s", key)
	}

	var val string
	if len(lst) > 0 {
		val = lst[len(lst)-1]
	}
	return val, nil
}

func (c *IniConfig) GetBool(key string) (bool, error) {
	val, err := c.GetItem(key)
	if err != nil {
		return false, err
	}
	return val == "true", nil
}

func (c *IniConfig) GetItems(key string) ([]string, error) {
	var vals []string
	if c == nil {
		return vals, fmt.Errorf("config is nil")
	}

	lst, ok := c.cfg[key]
	if !ok {
		return vals, fmt.Errorf("invalid key %s", key)
	}
	return lst, nil
}

func (c *IniConfig) getVal(key string) (string, bool) {
	if vals, ok := c.cfg[key]; ok && len(vals) > 0 {
		return vals[len(vals)-1], true
	}
	return "", false
}

func (c *IniConfig) setVal(key, val string) {
	if vals, ok := c.cfg[key]; ok && len(vals) > 0 {
		vals[len(vals)-1] = val
		return
	}
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
