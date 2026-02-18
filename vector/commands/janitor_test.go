package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setupJanitorTest creates a temp directory with a config file and test artifacts
// for the Janitor command. Returns the config file path and paths to verify.
func setupJanitorTest(t *testing.T) (configFile, imgOld, imgNew, dlFile, logFile string) {
	t.Helper()

	tmpDir := t.TempDir()

	confDir := filepath.Join(tmpDir, "conf")
	imagesDir := filepath.Join(tmpDir, "images")
	downloadsDir := filepath.Join(tmpDir, "downloads")
	logsDir := filepath.Join(tmpDir, "logs")
	logsWeeklyDir := filepath.Join(logsDir, "weekly-builder")

	for _, dir := range []string{confDir, imagesDir, downloadsDir, logsWeeklyDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("failed to create dir %s: %v", dir, err)
		}
	}

	imgOld = filepath.Join(imagesDir, "matrixos-20230101.img.xz")
	imgNew = filepath.Join(imagesDir, "matrixos-20230102.img.xz")
	dlFile = filepath.Join(downloadsDir, "some-download.tar.gz")
	logFile = filepath.Join(logsWeeklyDir, "build.log")

	for _, f := range []string{imgOld, imgNew, dlFile, logFile} {
		if err := os.WriteFile(f, []byte("dummy content"), 0644); err != nil {
			t.Fatalf("failed to create file %s: %v", f, err)
		}
	}

	// Make downloads and logs older than the default 30-day cutoff.
	archiveTime := time.Now().Add(-31 * 24 * time.Hour)
	if err := os.Chtimes(dlFile, archiveTime, archiveTime); err != nil {
		t.Fatalf("failed to set mtime on %s: %v", dlFile, err)
	}
	if err := os.Chtimes(logFile, archiveTime, archiveTime); err != nil {
		t.Fatalf("failed to set mtime on %s: %v", logFile, err)
	}

	configFile = filepath.Join(confDir, "matrixos.conf")
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
		t.Fatalf("failed to write config: %v", err)
	}
	return
}

func TestJanitorCommand(t *testing.T) {
	origGetEuid := getEuid
	getEuid = func() int { return 0 }
	defer func() { getEuid = origGetEuid }()

	configFile, imgOld, imgNew, dlFile, logFile := setupJanitorTest(t)

	cmd := NewJanitorCommand()
	if err := cmd.Init([]string{"--conf", configFile}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Capture stdout to avoid polluting test output
	captureStdout(t, func() {
		if err := cmd.Run(); err != nil {
			t.Fatalf("Run failed: %v", err)
		}
	})

	// Images: older image should be deleted, newer should remain
	if _, err := os.Stat(imgOld); !os.IsNotExist(err) {
		t.Errorf("Expected old image %s to be deleted", imgOld)
	}
	if _, err := os.Stat(imgNew); os.IsNotExist(err) {
		t.Errorf("Expected new image %s to exist", imgNew)
	}

	// Downloads older than 30 days should be deleted
	if _, err := os.Stat(dlFile); !os.IsNotExist(err) {
		t.Errorf("Expected download %s to be deleted", dlFile)
	}

	// Logs older than 30 days should be deleted
	if _, err := os.Stat(logFile); !os.IsNotExist(err) {
		t.Errorf("Expected log %s to be deleted", logFile)
	}
}

func TestJanitorNotRoot(t *testing.T) {
	origGetEuid := getEuid
	getEuid = func() int { return 1000 }
	defer func() { getEuid = origGetEuid }()

	configFile, _, _, _, _ := setupJanitorTest(t)

	cmd := NewJanitorCommand()
	if err := cmd.Init([]string{"--conf", configFile}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	err := cmd.Run()
	if err == nil {
		t.Fatal("Expected error for non-root user, got nil")
	}
	if err.Error() != "this command must be run as root" {
		t.Errorf("Unexpected error: %v", err)
	}
}
