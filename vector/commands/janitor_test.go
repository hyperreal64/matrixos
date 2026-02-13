package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestJanitorCommand creates an integration-style test for the Janitor command.
// It mocks the configuration via MATRIXOS_CONFIG and verifies file deletions.
func TestJanitorCommand(t *testing.T) {
	// Mock getEuid to simulate root
	origGetEuid := getEuid
	getEuid = func() int { return 0 }
	defer func() { getEuid = origGetEuid }()

	// Create temporary workspace
	tmpDir, err := os.MkdirTemp("", "janitor-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Define paths within temp workspace
	confDir := filepath.Join(tmpDir, "conf")
	imagesDir := filepath.Join(tmpDir, "images")
	downloadsDir := filepath.Join(tmpDir, "downloads")
	logsDir := filepath.Join(tmpDir, "logs")
	logsWeeklyDir := filepath.Join(logsDir, "weekly-builder")

	// Create directories
	for _, dir := range []string{confDir, imagesDir, downloadsDir, logsWeeklyDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}

	// Create dummy files to be cleaned
	// 1. Images: Keep 1, delete older
	imgOld := filepath.Join(imagesDir, "matrixos-20230101.img.xz")
	imgNew := filepath.Join(imagesDir, "matrixos-20230102.img.xz")
	// 2. Downloads: All deleted
	dlFile := filepath.Join(downloadsDir, "some-download.tar.gz")
	// 3. Logs: All deleted
	logFile := filepath.Join(logsWeeklyDir, "build.log")

	filesToCreate := []string{imgOld, imgNew, dlFile, logFile}
	for _, f := range filesToCreate {
		if err := os.WriteFile(f, []byte("dummy content"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Adjust mtime for downloads and logs to be older than 30 days
	// The default cutoff is 30 days.
	archiveTime := time.Now().Add(-31 * 24 * time.Hour)
	if err := os.Chtimes(dlFile, archiveTime, archiveTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(logFile, archiveTime, archiveTime); err != nil {
		t.Fatal(err)
	}

	// Create config file
	configFile := filepath.Join(confDir, "matrixos.conf")
	configContent := fmt.Sprintf(`
[Imager]
ImagesDir = %s

[Seeder]
DownloadsDir = %s

[matrixOS]
LogsDir = %s

[ImagesCleaner]
DryRun = false
MinAmountOfImages = 1

[DownloadsCleaner]
DryRun = false

[LogsCleaner]
DryRun = false
`, imagesDir, downloadsDir, logsDir)

	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Initialize and Run Janitor Command
	cmd := NewJanitorCommand()
	// Pass the config file path via the --conf flag
	if err := cmd.Init([]string{"--conf", configFile}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Capture stdout/stderr to avoid polluting test output
	// (Optional, but good practice)

	if err := cmd.Run(); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Verify deletions
	// 1. Images: Old should be gone, New should be present
	if _, err := os.Stat(imgOld); !os.IsNotExist(err) {
		t.Errorf("Expected old image %s to be deleted, but it exists", imgOld)
	}
	if _, err := os.Stat(imgNew); os.IsNotExist(err) {
		t.Errorf("Expected new image %s to exist, but it is missing", imgNew)
	}

	// 2. Downloads: Should be gone
	if _, err := os.Stat(dlFile); !os.IsNotExist(err) {
		t.Errorf("Expected download file %s to be deleted", dlFile)
	}

	// 3. Logs: Should be gone
	if _, err := os.Stat(logFile); !os.IsNotExist(err) {
		t.Errorf("Expected log file %s to be deleted", logFile)
	}
}
