package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIniConfig_Load_Expansion(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "matrixos-test-*.conf")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Define absolute paths for roots to ensure deterministic testsd
	rootPath := "/tmp/matrixos-root"
	privateRepoPath := "/tmp/matrixos-private"
	defaultPrivateRepoPath := "/tmp/matrixos-default-private"

	configContent := `
[matrixOS]
Root=` + rootPath + `
PrivateGitRepoPath=` + privateRepoPath + `
DefaultPrivateGitRepoPath=` + defaultPrivateRepoPath + `
LogsDir=/var/log/matrixos
LocksDir=locks

[Seeder]
DownloadsDir=out/seeder/downloads
DistfilesDir=out/seeder/distfiles
BinpkgsDir=out/seeder/binpkgs
PortageReposDir=out/seeder/repos
GpgKeysDir=out/seeder/gpg-keys
SecureBootPrivateKey=sb-keys/db.key
SecureBootPublicKey=sb-keys/db.pem
DefaultSecureBootPrivateKey=sb-keys/db.key
DefaultSecureBootPublicKey=sb-keys/db.pem
LocksDir=locks/seeder

[Releaser]
LocksDir=locks/releaser
HooksDir=release/hooks

[Imager]
LocksDir=locks/imager
ImagesDir=out/images
MountDir=out/mounts

[Ostree]
RepoDir=ostree/repo
DevGpgHomeDir=gpg-home
GpgPrivateKey=keys/priv.key
GpgPublicKey=keys/pub.key
GpgOfficialPublicKey=pubkeys/ostree.gpg
`
	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	params := ConfigFromPathParams{
		ConfigPath:  tmpFile.Name(),
		DefaultRoot: rootPath,
	}
	cfg, err := NewIniConfigFromPath(&params)
	if err != nil {
		t.Fatalf("Failed to create config: %v", err)
	}
	if err := cfg.Load(); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Helper to check values
	check := func(key, expected string) {
		val, err := cfg.GetItem(key)
		if err != nil {
			t.Errorf("GetItem(%q) returned error: %v", key, err)
			return
		}
		if val != expected {
			t.Errorf("Key %q: expected %q, got %q", key, expected, val)
		}
	}

	check("matrixOS.Root", rootPath)

	// Relative to matrixOS.Root
	check("matrixOS.PrivateGitRepoPath", privateRepoPath)
	check("matrixOS.DefaultPrivateGitRepoPath", defaultPrivateRepoPath)
	check("matrixOS.LocksDir", filepath.Join(rootPath, "locks"))
	check("matrixOS.LogsDir", "/var/log/matrixos")

	check("Seeder.LocksDir", filepath.Join(rootPath, "locks/seeder"))
	check("Seeder.DownloadsDir", filepath.Join(rootPath, "out/seeder/downloads"))
	check("Seeder.DistfilesDir", filepath.Join(rootPath, "out/seeder/distfiles"))
	check("Seeder.BinpkgsDir", filepath.Join(rootPath, "out/seeder/binpkgs"))
	check("Seeder.PortageReposDir", filepath.Join(rootPath, "out/seeder/repos"))
	check("Seeder.GpgKeysDir", filepath.Join(rootPath, "out/seeder/gpg-keys"))

	check("Releaser.LocksDir", filepath.Join(rootPath, "locks/releaser"))
	check("Releaser.HooksDir", filepath.Join(rootPath, "release/hooks"))

	check("Imager.LocksDir", filepath.Join(rootPath, "locks/imager"))
	check("Imager.ImagesDir", filepath.Join(rootPath, "out/images"))
	check("Imager.MountDir", filepath.Join(rootPath, "out/mounts"))

	check("Ostree.DevGpgHomeDir", filepath.Join(rootPath, "gpg-home"))
	check("Ostree.GpgOfficialPublicKey", filepath.Join(rootPath, "pubkeys/ostree.gpg"))
	check("Ostree.RepoDir", filepath.Join(rootPath, "ostree/repo"))

	// Relative to PrivateGitRepoPath
	check("Seeder.SecureBootPrivateKey", filepath.Join(privateRepoPath, "sb-keys/db.key"))
	check("Seeder.SecureBootPublicKey", filepath.Join(privateRepoPath, "sb-keys/db.pem"))
	check("Ostree.GpgPrivateKey", filepath.Join(privateRepoPath, "keys/priv.key"))
	check("Ostree.GpgPublicKey", filepath.Join(privateRepoPath, "keys/pub.key"))
	// Relative to DefaultPrivateGitRepoPath
	check("Seeder.DefaultSecureBootPrivateKey", filepath.Join(defaultPrivateRepoPath, "sb-keys/db.key"))
	check("Seeder.DefaultSecureBootPublicKey", filepath.Join(defaultPrivateRepoPath, "sb-keys/db.pem"))
}

func TestIniConfig_Defaults(t *testing.T) {
	// Test that defaults are applied when keys are missing
	tmpFile, err := os.CreateTemp("", "matrixos-test-defaults-*.conf")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	params := ConfigFromPathParams{
		ConfigPath:  tmpFile.Name(),
		DefaultRoot: filepath.Dir(tmpFile.Name()),
	}

	cfg, err := NewIniConfigFromPath(&params)
	if err != nil {
		t.Fatalf("Failed to create config: %v", err)
	}
	if err := cfg.Load(); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// matrixOS.Root should default to CWD (absolute)
	val, err := cfg.GetItem("matrixOS.Root")
	if err != nil {
		t.Errorf("GetItem(matrixOS.Root) error: %v", err)
	}
	if !filepath.IsAbs(val) {
		t.Errorf("Default matrixOS.Root should be absolute, got %q", val)
	}
}

func TestIniConfig_GetItem_LastValue(t *testing.T) {
	// Create an IniConfig manually with multiple values for a key
	cfg := &IniConfig{
		cfg: map[string][]string{
			"Test.Key": {"value1", "value2", "value3"},
		},
	}

	val, err := cfg.GetItem("Test.Key")
	if err != nil {
		t.Fatalf("GetItem returned error: %v", err)
	}

	expected := "value3"
	if val != expected {
		t.Errorf("GetItem returned %q, expected %q (last value)", val, expected)
	}
}

func TestIniConfig_GenerateSubConfigs(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir, err := os.MkdirTemp("", "matrixos-test-subconfig-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create the main config file
	configPath := filepath.Join(tmpDir, "matrixos.conf")
	configContent := `
[Section1]
Key1=Value1
Key2=Value2

[Section2]
Key3=Value3
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write main config: %v", err)
	}

	// Create the subconfig directory
	subConfigDir := configPath + ".d"
	if err := os.Mkdir(subConfigDir, 0755); err != nil {
		t.Fatalf("Failed to create subconfig dir: %v", err)
	}

	// Create an override config file
	overridePath := filepath.Join(subConfigDir, "00-override.conf")
	overrideContent := `
[Section1]
Key1=OverrideValue1
KeyNew=ValueNew
`
	if err := os.WriteFile(overridePath, []byte(overrideContent), 0644); err != nil {
		t.Fatalf("Failed to write override config: %v", err)
	}

	// Create another override config file
	override2Path := filepath.Join(subConfigDir, "10-override.conf")
	override2Content := `
[Section2]
Key3=OverrideValue3
`
	if err := os.WriteFile(override2Path, []byte(override2Content), 0644); err != nil {
		t.Fatalf("Failed to write override config 2: %v", err)
	}

	// Create a non-conf file to ensure it's ignored
	ignoredPath := filepath.Join(subConfigDir, "README.md")
	if err := os.WriteFile(ignoredPath, []byte("Ignore me"), 0644); err != nil {
		t.Fatalf("Failed to write ignored file: %v", err)
	}

	params := ConfigFromPathParams{
		ConfigPath:  configPath,
		DefaultRoot: tmpDir,
	}
	cfg, err := NewIniConfigFromPath(&params)
	if err != nil {
		t.Fatalf("Failed to create config: %v", err)
	}

	if err := cfg.Load(); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Helper to check values
	check := func(key, expected string) {
		val, err := cfg.GetItem(key)
		if err != nil {
			t.Errorf("GetItem(%q) returned error: %v", key, err)
			return
		}
		if val != expected {
			t.Errorf("Key %q: expected %q, got %q", key, expected, val)
		}
	}

	// Check main config values
	check("Section1.Key2", "Value2")

	// Check overridden values
	// Since the implementation uses a map[string][]string and appends,
	// GetItem returns the last one, which should be the override.
	check("Section1.Key1", "OverrideValue1")
	check("Section2.Key3", "OverrideValue3")

	// Check new value from subconfig
	check("Section1.KeyNew", "ValueNew")
}

func TestIniConfig_GenerateParent(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir, err := os.MkdirTemp("", "matrixos-test-parent-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create the parent config file
	parentContent := `
[matrixOS]
Root=/parent/root
LogsDir=logs-from-parent

[Seeder]
DownloadsDir=parent-downloads
BinpkgsDir=parent-binpkgs
`
	parentPath := filepath.Join(tmpDir, "parent.conf")
	if err := os.WriteFile(parentPath, []byte(parentContent), 0644); err != nil {
		t.Fatalf("Failed to write parent config: %v", err)
	}

	// Create the main config file that references the parent
	mainContent := `
[matrixOS]
ParentConfig=parent.conf
Root=/main/root
LogsDir=logs-from-main

[Seeder]
DownloadsDir=main-downloads
`
	mainPath := filepath.Join(tmpDir, "matrixos.conf")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("Failed to write main config: %v", err)
	}

	params := ConfigFromPathParams{
		ConfigPath:  mainPath,
		DefaultRoot: tmpDir,
	}
	cfg, err := NewIniConfigFromPath(&params)
	if err != nil {
		t.Fatalf("Failed to create config: %v", err)
	}
	if err := cfg.Load(); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	check := func(key, expected string) {
		t.Helper()
		val, err := cfg.GetItem(key)
		if err != nil {
			t.Errorf("GetItem(%q) returned error: %v", key, err)
			return
		}
		if val != expected {
			t.Errorf("Key %q: expected %q, got %q", key, expected, val)
		}
	}

	// Main config values should override parent values (last value wins).
	check("matrixOS.Root", "/main/root")
	check("matrixOS.LogsDir", "/main/root/logs-from-main")
	check("Seeder.DownloadsDir", "/main/root/main-downloads")

	// Values only present in the parent should still be accessible.
	check("Seeder.BinpkgsDir", "/main/root/parent-binpkgs")
}

func TestIniConfig_GenerateParent_MissingParentFile(t *testing.T) {
	// When ParentConfig references a file that doesn't exist, Load should
	// succeed (generateParent silently skips missing files).
	tmpDir, err := os.MkdirTemp("", "matrixos-test-parent-missing-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mainContent := `
[matrixOS]
ParentConfig=nonexistent.conf
Root=/some/root
`
	mainPath := filepath.Join(tmpDir, "matrixos.conf")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("Failed to write main config: %v", err)
	}

	params := ConfigFromPathParams{
		ConfigPath:  mainPath,
		DefaultRoot: tmpDir,
	}
	cfg, err := NewIniConfigFromPath(&params)
	if err != nil {
		t.Fatalf("Failed to create config: %v", err)
	}
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load should succeed when parent file is missing, got: %v", err)
	}

	val, err := cfg.GetItem("matrixOS.Root")
	if err != nil {
		t.Fatalf("GetItem(matrixOS.Root) error: %v", err)
	}
	if val != "/some/root" {
		t.Errorf("Expected /some/root, got %q", val)
	}
}

func TestIniConfig_GenerateParent_NoParentConfig(t *testing.T) {
	// When no ParentConfig key exists, generateParent is a no-op.
	tmpDir, err := os.MkdirTemp("", "matrixos-test-parent-nokey-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mainContent := `
[matrixOS]
Root=/some/root
LogsDir=logs
`
	mainPath := filepath.Join(tmpDir, "matrixos.conf")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("Failed to write main config: %v", err)
	}

	params := ConfigFromPathParams{
		ConfigPath:  mainPath,
		DefaultRoot: tmpDir,
	}
	cfg, err := NewIniConfigFromPath(&params)
	if err != nil {
		t.Fatalf("Failed to create config: %v", err)
	}
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	check := func(key, expected string) {
		t.Helper()
		val, err := cfg.GetItem(key)
		if err != nil {
			t.Errorf("GetItem(%q) error: %v", key, err)
			return
		}
		if val != expected {
			t.Errorf("Key %q: expected %q, got %q", key, expected, val)
		}
	}

	check("matrixOS.Root", "/some/root")
	check("matrixOS.LogsDir", "/some/root/logs")
}

func TestIniConfig_GenerateParent_WithSubConfigs(t *testing.T) {
	// Test the full chain: parent → main → sub-configs.
	// The priority order is: parent (lowest) < main < sub-configs (highest).
	tmpDir, err := os.MkdirTemp("", "matrixos-test-parent-sub-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Parent config: provides base values
	parentContent := `
[matrixOS]
Root=/chain/root

[Seeder]
DownloadsDir=parent-downloads
BinpkgsDir=parent-binpkgs
DistfilesDir=parent-distfiles
`
	parentPath := filepath.Join(tmpDir, "parent.conf")
	if err := os.WriteFile(parentPath, []byte(parentContent), 0644); err != nil {
		t.Fatalf("Failed to write parent config: %v", err)
	}

	// Main config: overrides some parent values, references parent
	mainContent := `
[matrixOS]
ParentConfig=parent.conf
Root=/chain/root

[Seeder]
DownloadsDir=main-downloads
`
	mainPath := filepath.Join(tmpDir, "matrixos.conf")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("Failed to write main config: %v", err)
	}

	// Sub-config directory
	subConfigDir := mainPath + ".d"
	if err := os.Mkdir(subConfigDir, 0755); err != nil {
		t.Fatalf("Failed to create subconfig dir: %v", err)
	}

	// Sub-config: overrides a value from main
	subContent := `
[Seeder]
DownloadsDir=sub-downloads
`
	subPath := filepath.Join(subConfigDir, "00-override.conf")
	if err := os.WriteFile(subPath, []byte(subContent), 0644); err != nil {
		t.Fatalf("Failed to write sub config: %v", err)
	}

	params := ConfigFromPathParams{
		ConfigPath:  mainPath,
		DefaultRoot: tmpDir,
	}
	cfg, err := NewIniConfigFromPath(&params)
	if err != nil {
		t.Fatalf("Failed to create config: %v", err)
	}
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	check := func(key, expected string) {
		t.Helper()
		val, err := cfg.GetItem(key)
		if err != nil {
			t.Errorf("GetItem(%q) error: %v", key, err)
			return
		}
		if val != expected {
			t.Errorf("Key %q: expected %q, got %q", key, expected, val)
		}
	}

	// Sub-config wins over main for DownloadsDir (last value wins via GetItem)
	check("Seeder.DownloadsDir", "/chain/root/sub-downloads")

	// Only in parent, inherited through the chain
	check("Seeder.BinpkgsDir", "/chain/root/parent-binpkgs")
	check("Seeder.DistfilesDir", "/chain/root/parent-distfiles")

	// setVal preserves history: parent, main, and sub-config entries are all kept.
	allDownloads, err := cfg.GetItems("Seeder.DownloadsDir")
	if err != nil {
		t.Fatalf("GetItems(Seeder.DownloadsDir) error: %v", err)
	}
	if len(allDownloads) != 3 {
		t.Errorf("Expected 3 history entries for Seeder.DownloadsDir, got %d: %v",
			len(allDownloads), allDownloads)
	}
}

func TestIniConfig_GenerateParent_ParentOverrideOrder(t *testing.T) {
	// Verify that the main config values take precedence over parent for
	// the same keys: parent is loaded first, then main appends on top.
	tmpDir, err := os.MkdirTemp("", "matrixos-test-parent-order-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	parentContent := `
[matrixOS]
Root=/parent/root

[Seeder]
DownloadsDir=from-parent
`
	if err := os.WriteFile(filepath.Join(tmpDir, "parent.conf"), []byte(parentContent), 0644); err != nil {
		t.Fatalf("Failed to write parent config: %v", err)
	}

	mainContent := `
[matrixOS]
ParentConfig=parent.conf
Root=/main/root

[Seeder]
DownloadsDir=from-main
`
	mainPath := filepath.Join(tmpDir, "main.conf")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("Failed to write main config: %v", err)
	}

	params := ConfigFromPathParams{
		ConfigPath:  mainPath,
		DefaultRoot: tmpDir,
	}
	cfg, err := NewIniConfigFromPath(&params)
	if err != nil {
		t.Fatalf("Failed to create config: %v", err)
	}
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// GetItem should return the main config value (last appended wins)
	val, err := cfg.GetItem("Seeder.DownloadsDir")
	if err != nil {
		t.Fatalf("GetItem error: %v", err)
	}
	expected := "/main/root/from-main"
	if val != expected {
		t.Errorf("Expected %q (main overrides parent), got %q", expected, val)
	}

	// setVal preserves history: both parent and main entries are kept.
	allVals, err := cfg.GetItems("Seeder.DownloadsDir")
	if err != nil {
		t.Fatalf("GetItems error: %v", err)
	}
	if len(allVals) != 2 {
		t.Fatalf("Expected 2 values (parent + main), got %d: %v", len(allVals), allVals)
	}
	// Last entry (main) should be expanded; first entry (parent) stays raw.
	if allVals[1] != expected {
		t.Errorf("Last value should be expanded main (%q), got %q", expected, allVals[1])
	}
}

func TestSearchPaths(t *testing.T) {
	// Create a temporary directory structure to test search path discovery
	tmpDir, err := os.MkdirTemp("", "matrixos-test-searchpaths-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create .matrixos marker file in the root of temp dir
	if err := os.WriteFile(filepath.Join(tmpDir, ".matrixos"), []byte(""), 0644); err != nil {
		t.Fatalf("Failed to create .matrixos file: %v", err)
	}

	// Create conf directory
	confDir := filepath.Join(tmpDir, "conf")
	if err := os.Mkdir(confDir, 0755); err != nil {
		t.Fatalf("Failed to create conf dir: %v", err)
	}

	// Create a subdirectory to run the test from
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	// Save current WD and deferred restore
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current working directory: %v", err)
	}
	// We need to change back
	defer func() {
		_ = os.Chdir(originalWd)
	}()

	// Change to subdir
	if err := os.Chdir(subDir); err != nil {
		t.Fatalf("Failed to change working directory: %v", err)
	}

	// helper to verify if we found our expected path
	found := false
	expectedPath, _ := filepath.EvalSymlinks(confDir)

	paths := searchPaths(BaseConfigFileName)
	for _, sp := range paths {
		// Resolve symlinks just in case tmp dir has them
		evalDirPath, err := filepath.EvalSymlinks(sp.dirPath)
		if err != nil {
			evalDirPath = sp.dirPath
		}

		if evalDirPath == expectedPath {
			found = true
			if sp.fileName != BaseConfigFileName {
				t.Errorf("Expected fileName %q, got %q", BaseConfigFileName, sp.fileName)
			}

			// Evaluated comparison for root as well
			evalRoot, _ := filepath.EvalSymlinks(sp.defaultRoot)
			evalTmp, _ := filepath.EvalSymlinks(tmpDir)
			if evalRoot != evalTmp {
				t.Errorf("Expected defaultRoot %q, got %q", evalTmp, evalRoot)
			}
			break
		}
	}

	if !found {
		t.Errorf("searchPaths did not find expected configuration directory: %s", expectedPath)
		for i, p := range paths {
			t.Logf("Search path %d: %+v", i, p)
		}
	}
}
