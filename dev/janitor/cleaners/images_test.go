package cleaners

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImagesCleaner_Name(t *testing.T) {
	cleaner := &ImagesCleaner{}
	if cleaner.Name() != "images" {
		t.Errorf("Expected name to be 'images', but got '%s'", cleaner.Name())
	}
}

func TestImagesCleaner_Init(t *testing.T) {
	cleaner := &ImagesCleaner{}
	mockConfig := &MockConfig{values: make(map[string]interface{})}
	err := cleaner.Init(mockConfig)
	if err != nil {
		t.Errorf("Init should not return an error, but got: %v", err)
	}
	if cleaner.cfg != mockConfig {
		t.Error("cfg should be initialized with the provided config")
	}
}

func TestImagesCleaner_isDryRun(t *testing.T) {
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
				mockConfig.values["ImagesCleaner.DryRun"] = tt.dryRun
			}
			cleaner := &ImagesCleaner{cfg: mockConfig}
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

func TestImagesCleaner_MinAmountOfImages(t *testing.T) {
	tests := []struct {
		name     string
		val      string
		expected int
		wantErr  bool
	}{
		{"Valid", "5", 5, false},
		{"Invalid", "abc", 0, true},
		{"NotSet", "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockConfig := &MockConfig{values: make(map[string]interface{})}
			if tt.name != "NotSet" {
				mockConfig.values["ImagesCleaner.MinAmountOfImages"] = tt.val
			}
			cleaner := &ImagesCleaner{cfg: mockConfig}
			got, err := cleaner.MinAmountOfImages()
			if (err != nil) != tt.wantErr {
				t.Errorf("MinAmountOfImages() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.expected {
				t.Errorf("MinAmountOfImages() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestImagesCleaner_Run(t *testing.T) {
	// Create dummy files
	files := []string{
		// Prefix 1: matrixos_test
		"matrixos_test-20260101.img.xz", // Oldest
		"matrixos_test-20260102.img.xz",
		"matrixos_test-20260103.img.xz",
		"matrixos_test-20260104.img.xz", // Newest
		// Associated files
		"matrixos_test-20260101.img.xz.asc",
		"matrixos_test-20260101.img.xz.sha256",

		// Prefix 2: matrixos_other (only 2 files, should keep all if min=2)
		"matrixos_other-20260101.img.xz",
		"matrixos_other-20260102.img.xz",

		// Invalid files
		"random_file.txt",
		"matrixos_invalid_date-202.img.xz",
	}

	tests := []struct {
		name            string
		dryRun          string
		minImages       string
		expectedDeleted []string
		expectedKept    []string
		wantErr         bool
	}{
		{
			name:      "RealRun_Min2",
			dryRun:    "false",
			minImages: "2",
			expectedDeleted: []string{
				"matrixos_test-20260101.img.xz",
				"matrixos_test-20260101.img.xz.asc",
				"matrixos_test-20260101.img.xz.sha256",
				"matrixos_test-20260102.img.xz",
			},
			expectedKept: []string{
				"matrixos_test-20260103.img.xz",
				"matrixos_test-20260104.img.xz",
				"matrixos_other-20260101.img.xz",
				"matrixos_other-20260102.img.xz",
				"random_file.txt",
			},
			wantErr: false,
		},
		{
			name:            "DryRun_Min2",
			dryRun:          "true",
			minImages:       "2",
			expectedDeleted: []string{}, // Nothing deleted
			expectedKept: []string{
				"matrixos_test-20260101.img.xz",
				"matrixos_test-20260102.img.xz",
				"matrixos_test-20260103.img.xz",
				"matrixos_test-20260104.img.xz",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subTempDir, err := os.MkdirTemp("", "test-images-sub-*")
			if err != nil {
				t.Fatalf("Failed to create sub temp dir: %v", err)
			}
			defer os.RemoveAll(subTempDir)

			for _, f := range files {
				path := filepath.Join(subTempDir, f)
				if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
					t.Fatalf("Failed to create file %s: %v", path, err)
				}
			}
			if err := os.Mkdir(filepath.Join(subTempDir, "subdir"), 0755); err != nil {
				t.Fatalf("Failed to create subdir: %v", err)
			}

			mockConfig := &MockConfig{values: make(map[string]interface{})}
			mockConfig.values["ImagesCleaner.DryRun"] = tt.dryRun
			mockConfig.values["ImagesCleaner.MinAmountOfImages"] = tt.minImages
			mockConfig.values["Imager.OutDir"] = subTempDir

			cleaner := &ImagesCleaner{}
			err = cleaner.Init(mockConfig)
			if err != nil {
				t.Fatalf("Init failed: %v", err)
			}
			err = cleaner.Run()

			if (err != nil) != tt.wantErr {
				t.Errorf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}

			for _, f := range tt.expectedDeleted {
				path := filepath.Join(subTempDir, f)
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Errorf("File %s should have been deleted", f)
				}
			}

			for _, f := range tt.expectedKept {
				path := filepath.Join(subTempDir, f)
				if _, err := os.Stat(path); os.IsNotExist(err) {
					t.Errorf("File %s should have been kept", f)
				}
			}
		})
	}
}
