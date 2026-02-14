package cleaners

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDownloadsCleaner_Name(t *testing.T) {
	cleaner := &DownloadsCleaner{}
	if cleaner.Name() != "downloads" {
		t.Errorf("Expected name to be 'downloads', but got '%s'", cleaner.Name())
	}
}

func TestDownloadsCleaner_Init(t *testing.T) {
	cleaner := &DownloadsCleaner{}
	mockConfig := &MockConfig{values: make(map[string]interface{})}
	err := cleaner.Init(mockConfig)
	if err != nil {
		t.Errorf("Init should not return an error, but got: %v", err)
	}
	if cleaner.cfg != mockConfig {
		t.Error("cfg should be initialized with the provided config")
	}
}

func TestDownloadsCleaner_isDryRun(t *testing.T) {
	tests := []struct {
		name     string
		dryRun   string
		expected bool
		wantErr  bool
	}{
		{"DryRunTrue", "true", true, false},
		{"DryRunFalse", "false", false, false},
		{"DryRunNotSet", "", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockConfig := &MockConfig{values: make(map[string]interface{})}
			if !tt.wantErr {
				mockConfig.values["DownloadsCleaner.DryRun"] = tt.dryRun
			}
			cleaner := &DownloadsCleaner{cfg: mockConfig}
			got, err := cleaner.isDryRun()
			if (err != nil) != tt.wantErr {
				t.Errorf("isDryRun() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.expected {
				t.Errorf("isDryRun() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDownloadsCleaner_getDownloadsDir(t *testing.T) {
	tests := []struct {
		name     string
		dir      string
		expected string
		wantErr  bool
	}{
		{"Valid", "/tmp/downloads", "/tmp/downloads", false},
		{"NotSet", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockConfig := &MockConfig{values: make(map[string]interface{})}
			if !tt.wantErr {
				mockConfig.values["Seeder.DownloadsDir"] = tt.dir
			}
			cleaner := &DownloadsCleaner{cfg: mockConfig}
			got, err := cleaner.getDownloadsDir()
			if (err != nil) != tt.wantErr {
				t.Errorf("getDownloadsDir() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.expected {
				t.Errorf("getDownloadsDir() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDownloadsCleaner_Run(t *testing.T) {
	tests := []struct {
		name            string
		dryRun          string
		expectedDeleted []string
		expectedKept    []string
		wantErr         bool
	}{
		{
			name:   "RealRun",
			dryRun: "false",
			expectedDeleted: []string{
				"old_file.txt",
			},
			expectedKept: []string{
				"new_file.txt",
			},
			wantErr: false,
		},
		{
			name:            "DryRun",
			dryRun:          "true",
			expectedDeleted: []string{},
			expectedKept: []string{
				"old_file.txt",
				"new_file.txt",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir, err := os.MkdirTemp("", "test-downloads-*")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tempDir)

			// Create files
			oldFile := filepath.Join(tempDir, "old_file.txt")
			if err := os.WriteFile(oldFile, []byte("old"), 0644); err != nil {
				t.Fatalf("Failed to create old file: %v", err)
			}
			// Set mtime to 31 days ago
			oldTime := time.Now().Add(-31 * 24 * time.Hour)
			if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
				t.Fatalf("Failed to chtimes old file: %v", err)
			}

			newFile := filepath.Join(tempDir, "new_file.txt")
			if err := os.WriteFile(newFile, []byte("new"), 0644); err != nil {
				t.Fatalf("Failed to create new file: %v", err)
			}
			// Set mtime to 1 day ago
			newTime := time.Now().Add(-1 * 24 * time.Hour)
			if err := os.Chtimes(newFile, newTime, newTime); err != nil {
				t.Fatalf("Failed to chtimes new file: %v", err)
			}

			mockConfig := &MockConfig{values: make(map[string]interface{})}
			mockConfig.values["DownloadsCleaner.DryRun"] = tt.dryRun
			mockConfig.values["Seeder.DownloadsDir"] = tempDir

			cleaner := &DownloadsCleaner{}
			err = cleaner.Init(mockConfig)
			if err != nil {
				t.Fatalf("Init failed: %v", err)
			}
			err = cleaner.Run()

			if (err != nil) != tt.wantErr {
				t.Errorf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}

			for _, f := range tt.expectedDeleted {
				path := filepath.Join(tempDir, f)
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Errorf("File %s should have been deleted", f)
				}
			}

			for _, f := range tt.expectedKept {
				path := filepath.Join(tempDir, f)
				if _, err := os.Stat(path); os.IsNotExist(err) {
					t.Errorf("File %s should have been kept", f)
				}
			}
		})
	}
}
