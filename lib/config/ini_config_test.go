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

	configContent := `
[matrixOS]
Root=` + rootPath + `
PrivateGitRepoPath=` + privateRepoPath + `
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
		if val.Item != expected {
			t.Errorf("Key %q: expected %q, got %q", key, expected, val.Item)
		}
	}

	check("matrixOS.Root", rootPath)

	// Relative to matrixOS.Root
	check("matrixOS.PrivateGitRepoPath", privateRepoPath)
	check("matrixOS.LocksDir", filepath.Join(rootPath, "locks"))
	check("Seeder.LocksDir", filepath.Join(rootPath, "locks/seeder"))
	check("Imager.LocksDir", filepath.Join(rootPath, "locks/imager"))
	check("Ostree.GpgOfficialPublicKey", filepath.Join(rootPath, "pubkeys/ostree.gpg"))
	check("matrixOS.LogsDir", "/var/log/matrixos")
	check("Ostree.DevGpgHomeDir", filepath.Join(rootPath, "gpg-home"))
	check("Imager.ImagesDir", filepath.Join(rootPath, "out/images"))
	check("Imager.MountDir", filepath.Join(rootPath, "out/mounts"))
	check("Seeder.DownloadsDir", filepath.Join(rootPath, "out/seeder/downloads"))
	check("Seeder.DistfilesDir", filepath.Join(rootPath, "out/seeder/distfiles"))
	check("Seeder.BinpkgsDir", filepath.Join(rootPath, "out/seeder/binpkgs"))
	check("Seeder.PortageReposDir", filepath.Join(rootPath, "out/seeder/repos"))
	check("Seeder.GpgKeysDir", filepath.Join(rootPath, "out/seeder/gpg-keys"))
	check("Ostree.RepoDir", filepath.Join(rootPath, "ostree/repo"))

	// Relative to PrivateGitRepoPath
	check("Seeder.SecureBootPrivateKey", filepath.Join(privateRepoPath, "sb-keys/db.key"))
	check("Seeder.SecureBootPublicKey", filepath.Join(privateRepoPath, "sb-keys/db.pem"))
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
	if !filepath.IsAbs(val.Item) {
		t.Errorf("Default matrixOS.Root should be absolute, got %q", val.Item)
	}
}
