package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIniConfig_Load_Expansion(t *testing.T) {
	// Create a temporary config file
	tmpFile, err := os.CreateTemp("", "matrixos-test-*.conf")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Define absolute paths for roots to ensure deterministic testsd
	rootPath := "/tmp/matrixos-root"
	privateRepoPath := "/tmp/matrixos-private"

	// Write config content that mimics matrixos.conf structure
	configContent := `
[matrixOS]
Root=` + rootPath + `
PrivateGitRepoPath=` + privateRepoPath + `
ArtifactsDir=artifacts
LogsDir=/var/log/matrixos
LocksDir=locks
OutDir=out

[Ostree]
Dir=ostree
RepoDir=repo
GpgOfficialPublicKey=pubkeys/ostree.gpg
DevGpgHomeDir=gpg-home
GpgPrivateKey=keys/priv.key
GpgPublicKey=keys/pub.key

[Seeder]
OutDir=seeder
DownloadsDir=downloads
DistfilesDir=distfiles
BinpkgsDir=binpkgs
PortageReposDir=repos
GpgKeysDir=gpg-keys
SecureBootPrivateKey=sb-keys/db.key
SecureBootPublicKey=sb-keys/db.pem

[Imager]
OutDir=images
`
	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	// Load config
	cfg := NewIniConfig(tmpFile.Name())
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

	// Level 0 (Roots)
	check("matrixOS.Root", rootPath)
	check("matrixOS.PrivateGitRepoPath", privateRepoPath)

	// Level 1 (Relative to Root)
	check("matrixOS.ArtifactsDir", filepath.Join(rootPath, "artifacts"))
	check("matrixOS.LocksDir", filepath.Join(rootPath, "locks"))
	check("Ostree.Dir", filepath.Join(rootPath, "ostree"))
	check("Ostree.GpgOfficialPublicKey", filepath.Join(rootPath, "pubkeys/ostree.gpg"))
	// Absolute path override check (LogsDir was set to /var/log/matrixos)
	check("matrixOS.LogsDir", "/var/log/matrixos")

	// Level 2 (Relative to ArtifactsDir)
	artifactsDir := filepath.Join(rootPath, "artifacts")
	check("matrixOS.OutDir", filepath.Join(artifactsDir, "out"))
	check("Ostree.DevGpgHomeDir", filepath.Join(artifactsDir, "gpg-home"))

	// Level 3 (Relative to OutDir)
	outDir := filepath.Join(artifactsDir, "out")
	check("Seeder.OutDir", filepath.Join(outDir, "seeder"))
	check("Imager.OutDir", filepath.Join(outDir, "images"))

	// Level 4 (Relative to Seeder.OutDir)
	seederOutDir := filepath.Join(outDir, "seeder")
	check("Seeder.DownloadsDir", filepath.Join(seederOutDir, "downloads"))
	check("Seeder.DistfilesDir", filepath.Join(seederOutDir, "distfiles"))
	check("Seeder.BinpkgsDir", filepath.Join(seederOutDir, "binpkgs"))
	check("Seeder.PortageReposDir", filepath.Join(seederOutDir, "repos"))
	check("Seeder.GpgKeysDir", filepath.Join(seederOutDir, "gpg-keys"))

	// Level X (Relative to PrivateGitRepoPath)
	check("Seeder.SecureBootPrivateKey", filepath.Join(privateRepoPath, "sb-keys/db.key"))
	check("Seeder.SecureBootPublicKey", filepath.Join(privateRepoPath, "sb-keys/db.pem"))
	check("Ostree.GpgPrivateKey", filepath.Join(privateRepoPath, "keys/priv.key"))
	check("Ostree.GpgPublicKey", filepath.Join(privateRepoPath, "keys/pub.key"))

	// Level Y (Relative to Ostree.Dir)
	ostreeDir := filepath.Join(rootPath, "ostree")
	check("Ostree.RepoDir", filepath.Join(ostreeDir, "repo"))
}

func TestIniConfig_Defaults(t *testing.T) {
	// Test that defaults are applied when keys are missing
	tmpFile, err := os.CreateTemp("", "matrixos-test-defaults-*.conf")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	cfg := NewIniConfig(tmpFile.Name())
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
