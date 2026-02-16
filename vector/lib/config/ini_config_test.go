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

	cfg, err := NewIniConfigFromFile(tmpFile.Name(), rootPath)
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
	check("Seeder.SecureBootPrivateKey", filepath.Join(defaultPrivateRepoPath, "sb-keys/db.key"))
	check("Seeder.SecureBootPublicKey", filepath.Join(defaultPrivateRepoPath, "sb-keys/db.pem"))
	check("Ostree.GpgPrivateKey", filepath.Join(privateRepoPath, "keys/priv.key"))
	check("Ostree.GpgPublicKey", filepath.Join(privateRepoPath, "keys/pub.key"))
}

func TestIniConfig_Defaults(t *testing.T) {
	// Test that defaults are applied when keys are missing
	tmpFile, err := os.CreateTemp("", "matrixos-test-defaults-*.conf")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	cfg, err := NewIniConfigFromFile(tmpFile.Name(), filepath.Dir(tmpFile.Name()))
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

	// Create and load the config
	cfg, err := NewIniConfigFromFile(configPath, tmpDir)
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
