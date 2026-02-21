package imager

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"matrixos/vector/lib/cds"
	"matrixos/vector/lib/config"
	"matrixos/vector/lib/testutil"
)

// baseImageConfig returns a mock config with all keys needed by Image.
func baseImageConfig() *config.MockConfig {
	return &config.MockConfig{
		Items: map[string][]string{
			"Imager.ImagesDir":                      {"/tmp/images"},
			"Imager.MountDir":                       {"/tmp/mnt"},
			"Imager.ImageSize":                      {"32G"},
			"Imager.EfiPartitionSize":               {"200M"},
			"Imager.BootPartitionSize":              {"1G"},
			"Imager.Compressor":                     {"xz -f -0 -T0"},
			"Imager.EspPartitionType":               {"C12A7328-F81F-11D2-BA4B-00A0C93EC93B"},
			"Imager.BootPartitionType":              {"BC13C2FF-59E6-4262-A352-B275FD6F7172"},
			"Imager.RootPartitionType":              {"4F68BCE3-E8CD-4DB1-96E7-FBCAF984B709"},
			"matrixOS.OsName":                       {"matrixos"},
			"Imager.BootRoot":                       {"/boot"},
			"Imager.EfiRoot":                        {"/efi"},
			"Imager.RelativeEfiBootPath":            {"EFI/BOOT"},
			"Imager.EfiExecutable":                  {"BOOTX64.EFI"},
			"Imager.EfiCertificateFileName":         {"secureboot.pem"},
			"Imager.EfiCertificateFileNameDer":      {"secureboot.der"},
			"Imager.EfiCertificateFileNameKek":      {"secureboot-kek.pem"},
			"Imager.EfiCertificateFileNameKekDer":   {"secureboot-kek.der"},
			"Releaser.ReadOnlyVdb":                  {"/usr/var-db-pkg"},
			"matrixOS.Root":                         {"/opt/matrixos"},
			"Imager.LocksDir":                       {"/tmp/locks"},
			"Imager.LockWaitSeconds":                {"300"},
			"Seeder.ChrootMetadataDir":              {"/etc/matrixos"},
			"Seeder.ChrootMetadataDirBuildFileName": {"build.txt"},
			"matrixOS.LogsDir":                      {"/tmp/logs"},
		},
	}
}

func newTestImage(cfg *config.MockConfig, ostree *cds.MockOstree) *Image {
	im, _ := NewImage(cfg, ostree)
	return im
}

func newTestImageWithRunner(cfg *config.MockConfig, ostree *cds.MockOstree, runner *testutil.MockRunner) *Image {
	im := newTestImage(cfg, ostree)
	im.runner = runner.Run
	return im
}

// --- Interface compliance ---

func TestImageImplementsIImage(t *testing.T) {
	var _ IImage = (*Image)(nil)
}

// --- NewImage Tests ---

func TestNewImage(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		im, err := NewImage(baseImageConfig(), &cds.MockOstree{})
		if err != nil {
			t.Fatalf("NewImage() error: %v", err)
		}
		if im == nil {
			t.Fatal("NewImage() returned nil")
		}
	})

	t.Run("NilConfig", func(t *testing.T) {
		_, err := NewImage(nil, &cds.MockOstree{})
		if err == nil {
			t.Fatal("expected error for nil config")
		}
	})

	t.Run("NilOstree", func(t *testing.T) {
		_, err := NewImage(baseImageConfig(), nil)
		if err == nil {
			t.Fatal("expected error for nil ostree")
		}
	})
}

// --- Config Accessor Tests ---

func TestConfigAccessors(t *testing.T) {
	cfg := baseImageConfig()
	im := newTestImage(cfg, &cds.MockOstree{})

	tests := []struct {
		name     string
		fn       func() (string, error)
		expected string
	}{
		{"ImagesOutDir", im.ImagesOutDir, "/tmp/images"},
		{"MountDir", im.MountDir, "/tmp/mnt"},
		{"ImageSize", im.ImageSize, "32G"},
		{"EfiPartitionSize", im.EfiPartitionSize, "200M"},
		{"BootPartitionSize", im.BootPartitionSize, "1G"},
		{"Compressor", im.Compressor, "xz -f -0 -T0"},
		{"EspPartitionType", im.EspPartitionType, "C12A7328-F81F-11D2-BA4B-00A0C93EC93B"},
		{"BootPartitionType", im.BootPartitionType, "BC13C2FF-59E6-4262-A352-B275FD6F7172"},
		{"RootPartitionType", im.RootPartitionType, "4F68BCE3-E8CD-4DB1-96E7-FBCAF984B709"},
		{"OsName", im.OsName, "matrixos"},
		{"BootRoot", im.BootRoot, "/boot"},
		{"EfiRoot", im.EfiRoot, "/efi"},
		{"RelativeEfiBootPath", im.RelativeEfiBootPath, "EFI/BOOT"},
		{"EfiExecutable", im.EfiExecutable, "BOOTX64.EFI"},
		{"EfiCertificateFileName", im.EfiCertificateFileName, "secureboot.pem"},
		{"EfiCertificateFileNameDer", im.EfiCertificateFileNameDer, "secureboot.der"},
		{"EfiCertificateFileNameKek", im.EfiCertificateFileNameKek, "secureboot-kek.pem"},
		{"EfiCertificateFileNameKekDer", im.EfiCertificateFileNameKekDer, "secureboot-kek.der"},
		{"ReadOnlyVdb", im.ReadOnlyVdb, "/usr/var-db-pkg"},
		{"DevDir", im.DevDir, "/opt/matrixos"},
		{"LockDir", im.LockDir, "/tmp/locks"},
		{"LockWaitSeconds", im.LockWaitSeconds, "300"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := tt.fn()
			if err != nil {
				t.Fatalf("%s() error: %v", tt.name, err)
			}
			if val != tt.expected {
				t.Errorf("%s() = %q, want %q", tt.name, val, tt.expected)
			}
		})
	}
}

func TestConfigAccessorsEmptyValue(t *testing.T) {
	accessors := []struct {
		key  string
		name string
		fn   func(*Image) (string, error)
	}{
		{"Imager.ImagesDir", "ImagesOutDir", func(im *Image) (string, error) { return im.ImagesOutDir() }},
		{"Imager.MountDir", "MountDir", func(im *Image) (string, error) { return im.MountDir() }},
		{"Imager.ImageSize", "ImageSize", func(im *Image) (string, error) { return im.ImageSize() }},
		{"matrixOS.OsName", "OsName", func(im *Image) (string, error) { return im.OsName() }},
		{"Imager.LocksDir", "LockDir", func(im *Image) (string, error) { return im.LockDir() }},
	}

	for _, tt := range accessors {
		t.Run(tt.name+"_Empty", func(t *testing.T) {
			cfg := baseImageConfig()
			cfg.Items[tt.key] = []string{""}
			im := newTestImage(cfg, &cds.MockOstree{})
			_, err := tt.fn(im)
			if err == nil {
				t.Errorf("%s() should return error for empty value", tt.name)
			}
		})
	}
}

func TestConfigAccessorsConfigError(t *testing.T) {
	ec := &config.ErrConfig{Err: errors.New("cfg error")}
	im, _ := NewImage(ec, &cds.MockOstree{})
	im.runner = testutil.NewMockRunner().Run

	accessors := []struct {
		name string
		fn   func() (string, error)
	}{
		{"ImagesOutDir", im.ImagesOutDir},
		{"MountDir", im.MountDir},
		{"ImageSize", im.ImageSize},
		{"OsName", im.OsName},
		{"BootRoot", im.BootRoot},
		{"EfiRoot", im.EfiRoot},
		{"LockDir", im.LockDir},
	}

	for _, tt := range accessors {
		t.Run(tt.name+"_ConfigError", func(t *testing.T) {
			_, err := tt.fn()
			if err == nil {
				t.Errorf("%s() should return error from broken config", tt.name)
			}
		})
	}
}

// --- BuildMetadataFile Tests ---

func TestBuildMetadataFile(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		cfg := baseImageConfig()
		im := newTestImage(cfg, &cds.MockOstree{})
		result, err := im.BuildMetadataFile()
		if err != nil {
			t.Fatalf("BuildMetadataFile() error: %v", err)
		}
		expected := filepath.Join("/etc/matrixos", "build.txt")
		if result != expected {
			t.Errorf("BuildMetadataFile() = %q, want %q", result, expected)
		}
	})

	t.Run("EmptyDir", func(t *testing.T) {
		cfg := baseImageConfig()
		cfg.Items["Seeder.ChrootMetadataDir"] = []string{""}
		im := newTestImage(cfg, &cds.MockOstree{})
		_, err := im.BuildMetadataFile()
		if err == nil {
			t.Error("should error for empty metadata dir")
		}
	})

	t.Run("EmptyFileName", func(t *testing.T) {
		cfg := baseImageConfig()
		cfg.Items["Seeder.ChrootMetadataDirBuildFileName"] = []string{""}
		im := newTestImage(cfg, &cds.MockOstree{})
		_, err := im.BuildMetadataFile()
		if err == nil {
			t.Error("should error for empty build file name")
		}
	})

	t.Run("ConfigError", func(t *testing.T) {
		ec := &config.ErrConfig{Err: errors.New("cfg error")}
		im, _ := NewImage(ec, &cds.MockOstree{})
		_, err := im.BuildMetadataFile()
		if err == nil {
			t.Error("should error from broken config")
		}
	})
}

// --- refToSuffix Tests ---

func TestRefToSuffix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"matrixos/amd64/gnome", "matrixos_amd64_gnome"},
		{"simple", "simple"},
		{"a/b/c/d", "a_b_c_d"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := refToSuffix(tt.input)
			if got != tt.expected {
				t.Errorf("refToSuffix(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// --- ImagePath Tests ---

func TestImagePath(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		result, err := im.ImagePath("matrixos/amd64/gnome")
		if err != nil {
			t.Fatalf("ImagePath() error: %v", err)
		}
		expected := "/tmp/images/matrixos_amd64_gnome.img"
		if result != expected {
			t.Errorf("ImagePath() = %q, want %q", result, expected)
		}
	})

	t.Run("StripsRemote", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		result, err := im.ImagePath("origin:matrixos/amd64/gnome")
		if err != nil {
			t.Fatalf("ImagePath() error: %v", err)
		}
		expected := "/tmp/images/matrixos_amd64_gnome.img"
		if result != expected {
			t.Errorf("ImagePath() = %q, want %q", result, expected)
		}
	})

	t.Run("EmptyRef", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		_, err := im.ImagePath("")
		if err == nil {
			t.Error("should error for empty ref")
		}
	})

	t.Run("ConfigError", func(t *testing.T) {
		ec := &config.ErrConfig{Err: errors.New("cfg error")}
		im, _ := NewImage(ec, &cds.MockOstree{})
		_, err := im.ImagePath("someref")
		if err == nil {
			t.Error("should error from broken config")
		}
	})
}

// --- ImagePathWithReleaseVersion Tests ---

func TestImagePathWithReleaseVersion(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		result, err := im.ImagePathWithReleaseVersion("matrixos/amd64/gnome", "20260221")
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		expected := "/tmp/images/matrixos_amd64_gnome-20260221.img"
		if result != expected {
			t.Errorf("got %q, want %q", result, expected)
		}
	})

	t.Run("EmptyRef", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		_, err := im.ImagePathWithReleaseVersion("", "20260221")
		if err == nil {
			t.Error("should error for empty ref")
		}
	})

	t.Run("EmptyReleaseVersion", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		_, err := im.ImagePathWithReleaseVersion("ref", "")
		if err == nil {
			t.Error("should error for empty releaseVersion")
		}
	})
}

// --- ImagePathWithCompressorExtension Tests ---

func TestImagePathWithCompressorExtension(t *testing.T) {
	im := newTestImage(baseImageConfig(), &cds.MockOstree{})

	t.Run("XZ", func(t *testing.T) {
		result, err := im.ImagePathWithCompressorExtension("/tmp/test.img", "xz -f -0 -T0")
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if result != "/tmp/test.img.xz" {
			t.Errorf("got %q, want /tmp/test.img.xz", result)
		}
	})

	t.Run("Zstd", func(t *testing.T) {
		result, err := im.ImagePathWithCompressorExtension("/tmp/test.img", "zstd -3")
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if result != "/tmp/test.img.zstd" {
			t.Errorf("got %q, want /tmp/test.img.zstd", result)
		}
	})

	t.Run("EmptyPath", func(t *testing.T) {
		_, err := im.ImagePathWithCompressorExtension("", "xz")
		if err == nil {
			t.Error("should error for empty path")
		}
	})

	t.Run("EmptyCompressor", func(t *testing.T) {
		_, err := im.ImagePathWithCompressorExtension("/tmp/x.img", "")
		if err == nil {
			t.Error("should error for empty compressor")
		}
	})
}

// --- CreateImage Tests ---

func TestCreateImage(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		tmpDir := t.TempDir()
		imagePath := filepath.Join(tmpDir, "subdir", "test.img")
		runner := testutil.NewMockRunner()
		im := newTestImageWithRunner(baseImageConfig(), &cds.MockOstree{}, runner)

		err := im.CreateImage(imagePath, "32G")
		if err != nil {
			t.Fatalf("CreateImage() error: %v", err)
		}
		// Should have called truncate.
		if len(runner.Calls) != 1 {
			t.Fatalf("expected 1 runner call, got %d", len(runner.Calls))
		}
		if runner.Calls[0].Name != "truncate" {
			t.Errorf("expected truncate, got %q", runner.Calls[0].Name)
		}
	})

	t.Run("EmptyPath", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		err := im.CreateImage("", "32G")
		if err == nil {
			t.Error("should error for empty imagePath")
		}
	})

	t.Run("EmptySize", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		err := im.CreateImage("/tmp/test.img", "")
		if err == nil {
			t.Error("should error for empty imageSize")
		}
	})

	t.Run("TruncateFails", func(t *testing.T) {
		tmpDir := t.TempDir()
		imagePath := filepath.Join(tmpDir, "test.img")
		runner := testutil.NewMockRunnerFailOnCall(0, errors.New("truncate failed"))
		im := newTestImageWithRunner(baseImageConfig(), &cds.MockOstree{}, runner)

		err := im.CreateImage(imagePath, "32G")
		if err == nil {
			t.Error("should propagate truncate error")
		}
	})
}

// --- CompressImage Tests ---

func TestCompressImage(t *testing.T) {
	t.Run("EmptyPath", func(t *testing.T) {
		runner := testutil.NewMockRunner()
		im := newTestImageWithRunner(baseImageConfig(), &cds.MockOstree{}, runner)
		err := im.CompressImage("", "xz -f")
		if err == nil {
			t.Error("should error for empty imagePath")
		}
	})

	t.Run("EmptyCompressor", func(t *testing.T) {
		runner := testutil.NewMockRunner()
		im := newTestImageWithRunner(baseImageConfig(), &cds.MockOstree{}, runner)
		err := im.CompressImage("/tmp/test.img", "")
		if err == nil {
			t.Error("should error for empty compressor")
		}
	})

	t.Run("CommandArgs", func(t *testing.T) {
		tmpDir := t.TempDir()
		imgPath := filepath.Join(tmpDir, "test.img")
		// Create the expected output file so the existence check passes.
		os.WriteFile(imgPath+".xz", []byte("compressed"), 0644)

		runner := testutil.NewMockRunner()
		im := newTestImageWithRunner(baseImageConfig(), &cds.MockOstree{}, runner)

		err := im.CompressImage(imgPath, "xz -f -0 -T0")
		if err != nil {
			t.Fatalf("CompressImage() error: %v", err)
		}
		if len(runner.Calls) < 1 {
			t.Fatal("expected at least 1 runner call")
		}
		if runner.Calls[0].Name != "xz" {
			t.Errorf("expected xz command, got %q", runner.Calls[0].Name)
		}
		args := runner.Calls[0].Args
		// Args should be [-f -0 -T0 <imgPath>].
		if len(args) != 4 {
			t.Fatalf("expected 4 args, got %d: %v", len(args), args)
		}
		if args[len(args)-1] != imgPath {
			t.Errorf("last arg should be image path, got %q", args[len(args)-1])
		}
	})
}

// --- ClearPartitionTable Tests ---

func TestClearPartitionTable(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		runner := testutil.NewMockRunner()
		im := newTestImageWithRunner(baseImageConfig(), &cds.MockOstree{}, runner)

		err := im.ClearPartitionTable("/dev/sda")
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if len(runner.Calls) != 2 {
			t.Fatalf("expected 2 sgdisk calls, got %d", len(runner.Calls))
		}
		if runner.Calls[0].Name != "sgdisk" {
			t.Errorf("call 0: expected sgdisk, got %q", runner.Calls[0].Name)
		}
		if runner.Calls[1].Name != "sgdisk" {
			t.Errorf("call 1: expected sgdisk, got %q", runner.Calls[1].Name)
		}
	})

	t.Run("EmptyDevice", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		err := im.ClearPartitionTable("")
		if err == nil {
			t.Error("should error for empty devicePath")
		}
	})

	t.Run("FirstSgdiskFails", func(t *testing.T) {
		runner := testutil.NewMockRunnerFailOnCall(0, errors.New("sgdisk error"))
		im := newTestImageWithRunner(baseImageConfig(), &cds.MockOstree{}, runner)

		err := im.ClearPartitionTable("/dev/sda")
		if err == nil {
			t.Error("should propagate sgdisk error")
		}
	})
}

// --- DatedFsLabel Tests ---

func TestDatedFsLabel(t *testing.T) {
	im := newTestImage(baseImageConfig(), &cds.MockOstree{})
	label := im.DatedFsLabel()
	expected := time.Now().Format("20060102")
	if label != expected {
		t.Errorf("DatedFsLabel() = %q, want %q", label, expected)
	}
}

// --- PartitionDevices Tests ---

func TestPartitionDevices(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		runner := testutil.NewMockRunner()
		im := newTestImageWithRunner(baseImageConfig(), &cds.MockOstree{}, runner)

		err := im.PartitionDevices("200M", "1G", "32G", "/dev/loop0")
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		// 4 sgdisk calls + 1 partprobe = 5.
		if len(runner.Calls) != 5 {
			t.Fatalf("expected 5 runner calls, got %d", len(runner.Calls))
		}
		commands := make([]string, len(runner.Calls))
		for i, c := range runner.Calls {
			commands[i] = c.Name
		}
		if commands[0] != "sgdisk" || commands[1] != "sgdisk" || commands[2] != "sgdisk" || commands[3] != "sgdisk" {
			t.Errorf("expected 4 sgdisk calls, got %v", commands[:4])
		}
		if commands[4] != "partprobe" {
			t.Errorf("expected partprobe call, got %q", commands[4])
		}
	})

	t.Run("EmptyParams", func(t *testing.T) {
		runner := testutil.NewMockRunner()
		im := newTestImageWithRunner(baseImageConfig(), &cds.MockOstree{}, runner)

		if err := im.PartitionDevices("", "1G", "32G", "/dev/x"); err == nil {
			t.Error("should error for empty efiSize")
		}
		if err := im.PartitionDevices("200M", "", "32G", "/dev/x"); err == nil {
			t.Error("should error for empty bootSize")
		}
		if err := im.PartitionDevices("200M", "1G", "", "/dev/x"); err == nil {
			t.Error("should error for empty imageSize")
		}
		if err := im.PartitionDevices("200M", "1G", "32G", ""); err == nil {
			t.Error("should error for empty devicePath")
		}
	})

	t.Run("SgdiskFails", func(t *testing.T) {
		runner := testutil.NewMockRunnerFailOnCall(0, errors.New("sgdisk failed"))
		im := newTestImageWithRunner(baseImageConfig(), &cds.MockOstree{}, runner)

		err := im.PartitionDevices("200M", "1G", "32G", "/dev/loop0")
		if err == nil {
			t.Error("should propagate sgdisk error")
		}
	})

	t.Run("ConfigError", func(t *testing.T) {
		ec := &config.ErrConfig{Err: errors.New("cfg error")}
		im, _ := NewImage(ec, &cds.MockOstree{})
		runner := testutil.NewMockRunner()
		im.runner = runner.Run

		err := im.PartitionDevices("200M", "1G", "32G", "/dev/loop0")
		if err == nil {
			t.Error("should error from broken config")
		}
	})
}

// --- FormatEfifs Tests ---

func TestFormatEfifs(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		runner := testutil.NewMockRunner()
		im := newTestImageWithRunner(baseImageConfig(), &cds.MockOstree{}, runner)

		err := im.FormatEfifs("/dev/loop0p1")
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if len(runner.Calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(runner.Calls))
		}
		if runner.Calls[0].Name != "mkfs.vfat" {
			t.Errorf("expected mkfs.vfat, got %q", runner.Calls[0].Name)
		}
	})

	t.Run("Empty", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		if err := im.FormatEfifs(""); err == nil {
			t.Error("should error for empty device")
		}
	})
}

// --- MountEfifs Tests ---

func TestMountEfifs(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		tmpDir := t.TempDir()
		mountPoint := filepath.Join(tmpDir, "efi")
		runner := testutil.NewMockRunner()
		im := newTestImageWithRunner(baseImageConfig(), &cds.MockOstree{}, runner)

		err := im.MountEfifs("/dev/loop0p1", mountPoint)
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if len(runner.Calls) != 1 || runner.Calls[0].Name != "mount" {
			t.Errorf("expected mount call, got %v", runner.Calls)
		}
	})

	t.Run("EmptyParams", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		if err := im.MountEfifs("", "/tmp/efi"); err == nil {
			t.Error("should error for empty device")
		}
		if err := im.MountEfifs("/dev/x", ""); err == nil {
			t.Error("should error for empty mount point")
		}
	})
}

// --- FormatBootfs Tests ---

func TestFormatBootfs(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		runner := testutil.NewMockRunner()
		im := newTestImageWithRunner(baseImageConfig(), &cds.MockOstree{}, runner)

		err := im.FormatBootfs("/dev/loop0p2")
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if runner.Calls[0].Name != "mkfs.btrfs" {
			t.Errorf("expected mkfs.btrfs, got %q", runner.Calls[0].Name)
		}
	})

	t.Run("Empty", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		if err := im.FormatBootfs(""); err == nil {
			t.Error("should error for empty device")
		}
	})
}

// --- MountBootfs Tests ---

func TestMountBootfs(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		tmpDir := t.TempDir()
		mountPoint := filepath.Join(tmpDir, "boot")
		runner := testutil.NewMockRunner()
		im := newTestImageWithRunner(baseImageConfig(), &cds.MockOstree{}, runner)

		err := im.MountBootfs("/dev/loop0p2", mountPoint)
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if len(runner.Calls) != 1 || runner.Calls[0].Name != "mount" {
			t.Errorf("expected mount call, got %v", runner.Calls)
		}
	})

	t.Run("EmptyParams", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		if err := im.MountBootfs("", "/boot"); err == nil {
			t.Error("should error for empty device")
		}
		if err := im.MountBootfs("/dev/x", ""); err == nil {
			t.Error("should error for empty mount point")
		}
	})
}

// --- FormatRootfs Tests ---

func TestFormatRootfs(t *testing.T) {
	runner := testutil.NewMockRunner()
	im := newTestImageWithRunner(baseImageConfig(), &cds.MockOstree{}, runner)

	err := im.FormatRootfs("/dev/loop0p3")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if runner.Calls[0].Name != "mkfs.btrfs" {
		t.Errorf("expected mkfs.btrfs, got %q", runner.Calls[0].Name)
	}
}

// --- RootfsKernelArgs Tests ---

func TestRootfsKernelArgs(t *testing.T) {
	im := newTestImage(baseImageConfig(), &cds.MockOstree{})
	args := im.RootfsKernelArgs()
	if len(args) != 1 || args[0] != "rootflags=discard=async" {
		t.Errorf("unexpected kernel args: %v", args)
	}
}

// --- MountRootfs Tests ---

func TestMountRootfs(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		runner := testutil.NewMockRunner()
		im := newTestImageWithRunner(baseImageConfig(), &cds.MockOstree{}, runner)

		err := im.MountRootfs("/dev/loop0p3", "/tmp/rootfs")
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if runner.Calls[0].Name != "mount" {
			t.Errorf("expected mount, got %q", runner.Calls[0].Name)
		}
		// Check btrfs options.
		found := false
		for _, arg := range runner.Calls[0].Args {
			if strings.Contains(arg, "compress-force=zstd:6") {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected btrfs compression options in mount args")
		}
	})

	t.Run("EmptyParams", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		if err := im.MountRootfs("", "/tmp/mnt"); err == nil {
			t.Error("should error for empty rootDevice")
		}
		if err := im.MountRootfs("/dev/x", ""); err == nil {
			t.Error("should error for empty mountRootfs")
		}
	})
}

// --- GetKernelPath Tests ---

func TestGetKernelPath(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		tmpDir := t.TempDir()
		modulesDir := filepath.Join(tmpDir, "usr", "lib", "modules")
		os.MkdirAll(filepath.Join(modulesDir, "6.1.0-matrixos"), 0755)
		os.MkdirAll(filepath.Join(modulesDir, "6.2.0-matrixos"), 0755)

		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		result, err := im.GetKernelPath(tmpDir)
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		// Should return the first sorted (6.1.0).
		if result != "6.1.0-matrixos" {
			t.Errorf("got %q, want 6.1.0-matrixos", result)
		}
	})

	t.Run("NoModulesDir", func(t *testing.T) {
		tmpDir := t.TempDir()
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		_, err := im.GetKernelPath(tmpDir)
		if err == nil {
			t.Error("should error when modules dir doesn't exist")
		}
	})

	t.Run("EmptyModulesDir", func(t *testing.T) {
		tmpDir := t.TempDir()
		os.MkdirAll(filepath.Join(tmpDir, "usr", "lib", "modules"), 0755)
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		_, err := im.GetKernelPath(tmpDir)
		if err == nil {
			t.Error("should error for empty modules dir")
		}
	})

	t.Run("EmptyParam", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		_, err := im.GetKernelPath("")
		if err == nil {
			t.Error("should error for empty param")
		}
	})
}

// --- SetupPasswords Tests ---

func TestSetupPasswords(t *testing.T) {
	t.Run("EmptyParam", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		err := im.SetupPasswords("")
		if err == nil {
			t.Error("should error for empty param")
		}
	})
}

// --- ReleaseVersion Tests ---

func TestReleaseVersion(t *testing.T) {
	t.Run("FallbackToDate", func(t *testing.T) {
		tmpDir := t.TempDir()
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		result, err := im.ReleaseVersion(tmpDir)
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		expected := time.Now().Format("20060102")
		if result != expected {
			t.Errorf("got %q, want %q", result, expected)
		}
	})

	t.Run("FromMetadata", func(t *testing.T) {
		tmpDir := t.TempDir()
		metadataDir := filepath.Join(tmpDir, "etc", "matrixos")
		os.MkdirAll(metadataDir, 0755)
		os.WriteFile(filepath.Join(metadataDir, "build.txt"),
			[]byte("SEED_NAME=matrixos-gnome-20260215\nBUILD_DATE=2026-02-15\n"), 0644)

		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		result, err := im.ReleaseVersion(tmpDir)
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if result != "20260215" {
			t.Errorf("got %q, want 20260215", result)
		}
	})

	t.Run("EmptyRootfs", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		_, err := im.ReleaseVersion("")
		if err == nil {
			t.Error("should error for empty rootfs")
		}
	})

	t.Run("ConfigError", func(t *testing.T) {
		ec := &config.ErrConfig{Err: errors.New("cfg error")}
		im, _ := NewImage(ec, &cds.MockOstree{})
		_, err := im.ReleaseVersion("/tmp/rootfs")
		if err == nil {
			t.Error("should error from broken config")
		}
	})
}

// --- Qcow2ImagePath Tests ---

func TestQcow2ImagePath(t *testing.T) {
	im := newTestImage(baseImageConfig(), &cds.MockOstree{})

	t.Run("Success", func(t *testing.T) {
		result, err := im.Qcow2ImagePath("/tmp/images/test.img")
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if result != "/tmp/images/test.img.qcow2" {
			t.Errorf("got %q, want /tmp/images/test.img.qcow2", result)
		}
	})

	t.Run("Empty", func(t *testing.T) {
		_, err := im.Qcow2ImagePath("")
		if err == nil {
			t.Error("should error for empty path")
		}
	})
}

// --- CreateQcow2Image Tests ---

func TestCreateQcow2Image(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		runner := testutil.NewMockRunner()
		im := newTestImageWithRunner(baseImageConfig(), &cds.MockOstree{}, runner)

		err := im.CreateQcow2Image("/tmp/images/test.img")
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if len(runner.Calls) != 1 || runner.Calls[0].Name != "qemu-img" {
			t.Errorf("expected qemu-img call, got %v", runner.Calls)
		}
		// Verify output path ends with .qcow2.
		args := runner.Calls[0].Args
		if args[len(args)-1] != "/tmp/images/test.img.qcow2" {
			t.Errorf("last arg should be qcow2 path, got %q", args[len(args)-1])
		}
	})

	t.Run("Empty", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		err := im.CreateQcow2Image("")
		if err == nil {
			t.Error("should error for empty imagePath")
		}
	})
}

// --- RemoveImageFile Tests ---

func TestRemoveImageFile(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		tmpDir := t.TempDir()
		imgPath := filepath.Join(tmpDir, "test.img")
		os.WriteFile(imgPath, []byte("data"), 0644)
		os.WriteFile(imgPath+".sha256", []byte("hash"), 0644)
		os.WriteFile(imgPath+".asc", []byte("sig"), 0644)

		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		err := im.RemoveImageFile(imgPath)
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		for _, p := range []string{imgPath, imgPath + ".sha256", imgPath + ".asc"} {
			if _, err := os.Stat(p); !os.IsNotExist(err) {
				t.Errorf("%s should have been removed", p)
			}
		}
	})

	t.Run("NonexistentFile", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		err := im.RemoveImageFile("/tmp/nonexistent.img")
		if err != nil {
			t.Error("should not error when file doesn't exist")
		}
	})

	t.Run("Empty", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		err := im.RemoveImageFile("")
		if err == nil {
			t.Error("should error for empty path")
		}
	})
}

// --- ImageLockDir Tests ---

func TestImageLockDir(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockDir := filepath.Join(tmpDir, "locks")
		cfg := baseImageConfig()
		cfg.Items["Imager.LocksDir"] = []string{lockDir}
		im := newTestImage(cfg, &cds.MockOstree{})

		result, err := im.ImageLockDir()
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if result != lockDir {
			t.Errorf("got %q, want %q", result, lockDir)
		}
		// Verify directory was created.
		if _, err := os.Stat(lockDir); os.IsNotExist(err) {
			t.Error("lock directory should have been created")
		}
	})

	t.Run("ConfigError", func(t *testing.T) {
		ec := &config.ErrConfig{Err: errors.New("cfg error")}
		im, _ := NewImage(ec, &cds.MockOstree{})
		_, err := im.ImageLockDir()
		if err == nil {
			t.Error("should error from broken config")
		}
	})
}

// --- ImageLockPath Tests ---

func TestImageLockPath(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockDir := filepath.Join(tmpDir, "locks")
		cfg := baseImageConfig()
		cfg.Items["Imager.LocksDir"] = []string{lockDir}
		im := newTestImage(cfg, &cds.MockOstree{})

		result, err := im.ImageLockPath("matrixos/amd64/gnome")
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		expected := filepath.Join(lockDir, "matrixos/amd64/gnome.lock")
		if result != expected {
			t.Errorf("got %q, want %q", result, expected)
		}
	})

	t.Run("EmptyRef", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		_, err := im.ImageLockPath("")
		if err == nil {
			t.Error("should error for empty ref")
		}
	})
}

// --- FinalizeFilesystems Tests ---

func TestFinalizeFilesystems(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		runner := testutil.NewMockRunner()
		im := newTestImageWithRunner(baseImageConfig(), &cds.MockOstree{}, runner)

		err := im.FinalizeFilesystems("/mnt/rootfs", "/mnt/boot", "/mnt/efi")
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if len(runner.Calls) != 2 {
			t.Fatalf("expected 2 fstrim calls, got %d", len(runner.Calls))
		}
		for _, c := range runner.Calls {
			if c.Name != "fstrim" {
				t.Errorf("expected fstrim, got %q", c.Name)
			}
		}
	})

	t.Run("EmptyParams", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		if err := im.FinalizeFilesystems("", "/mnt/boot", "/mnt/efi"); err == nil {
			t.Error("should error for empty mountRootfs")
		}
		if err := im.FinalizeFilesystems("/mnt/rootfs", "", "/mnt/efi"); err == nil {
			t.Error("should error for empty mountBootfs")
		}
		if err := im.FinalizeFilesystems("/mnt/rootfs", "/mnt/boot", ""); err == nil {
			t.Error("should error for empty mountEfifs")
		}
	})
}

// --- ShowFinalFilesystemInfo Tests ---

func TestShowFinalFilesystemInfo(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		runner := testutil.NewMockRunner()
		im := newTestImageWithRunner(baseImageConfig(), &cds.MockOstree{}, runner)

		err := im.ShowFinalFilesystemInfo("/dev/loop0", "/mnt/boot", "/mnt/efi")
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		// find (boot) + find (efi) + blkid = 3 calls.
		if len(runner.Calls) != 3 {
			t.Fatalf("expected 3 runner calls, got %d", len(runner.Calls))
		}
	})

	t.Run("EmptyParams", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		if err := im.ShowFinalFilesystemInfo("", "/a", "/b"); err == nil {
			t.Error("should error for empty blockDevice")
		}
		if err := im.ShowFinalFilesystemInfo("/dev/x", "", "/b"); err == nil {
			t.Error("should error for empty mountBootfs")
		}
		if err := im.ShowFinalFilesystemInfo("/dev/x", "/a", ""); err == nil {
			t.Error("should error for empty mountEfifs")
		}
	})
}

// --- ShowTestInfo Tests ---

func TestShowTestInfo(t *testing.T) {
	im := newTestImage(baseImageConfig(), &cds.MockOstree{})
	// Should not panic with valid artifacts.
	im.ShowTestInfo([]string{"/tmp/test.img", "/tmp/test.img.xz"})
	// Should not panic with empty artifacts.
	im.ShowTestInfo(nil)
}

// --- PackageList Tests ---

func TestPackageList(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		tmpDir := t.TempDir()
		vdb := filepath.Join(tmpDir, "usr", "var-db-pkg")
		os.MkdirAll(filepath.Join(vdb, "sys-libs", "glibc-2.38"), 0755)
		os.MkdirAll(filepath.Join(vdb, "dev-libs", "openssl-3.0"), 0755)
		os.MkdirAll(filepath.Join(vdb, "app-misc", "screen-4.9"), 0755)

		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		result, err := im.PackageList(tmpDir)
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if len(result) != 3 {
			t.Fatalf("expected 3 packages, got %d: %v", len(result), result)
		}
	})

	t.Run("VdbNotExists", func(t *testing.T) {
		tmpDir := t.TempDir()
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		result, err := im.PackageList(tmpDir)
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if result != nil {
			t.Errorf("expected nil for non-existent VDB, got %v", result)
		}
	})

	t.Run("Empty", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		_, err := im.PackageList("")
		if err == nil {
			t.Error("should error for empty rootfs")
		}
	})

	t.Run("ConfigError", func(t *testing.T) {
		ec := &config.ErrConfig{Err: errors.New("cfg error")}
		im, _ := NewImage(ec, &cds.MockOstree{})
		_, err := im.PackageList("/tmp/rootfs")
		if err == nil {
			t.Error("should error from broken config")
		}
	})
}

// --- SetupHooks Tests ---

func TestSetupHooks(t *testing.T) {
	t.Run("EmptyParams", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		if err := im.SetupHooks("", "ref"); err == nil {
			t.Error("should error for empty ostreeDeployRootfs")
		}
		if err := im.SetupHooks("/tmp/rootfs", ""); err == nil {
			t.Error("should error for empty ref")
		}
	})

	t.Run("NoHooksDir", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := baseImageConfig()
		cfg.Items["matrixOS.Root"] = []string{tmpDir}
		im := newTestImage(cfg, &cds.MockOstree{})
		// Should return nil when hooks dir doesn't exist.
		err := im.SetupHooks("/tmp/rootfs", "matrixos/amd64/gnome")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("NoHookScript", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := baseImageConfig()
		cfg.Items["matrixOS.Root"] = []string{tmpDir}
		os.MkdirAll(filepath.Join(tmpDir, "image", "hooks"), 0755)
		im := newTestImage(cfg, &cds.MockOstree{})

		err := im.SetupHooks("/tmp/rootfs", "matrixos/amd64/gnome")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("OstreeError", func(t *testing.T) {
		mo := &cds.MockOstree{RemoveFullErr: errors.New("ostree error")}
		im := newTestImage(baseImageConfig(), mo)
		err := im.SetupHooks("/tmp/rootfs", "ref")
		if err == nil {
			t.Error("should propagate ostree error")
		}
	})
}

// --- TestImage Tests ---

func TestTestImageMethod(t *testing.T) {
	t.Run("EmptyParams", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		if err := im.TestImage("", "ref"); err == nil {
			t.Error("should error for empty imagePath")
		}
		if err := im.TestImage("/tmp/x.img", ""); err == nil {
			t.Error("should error for empty ref")
		}
	})

	t.Run("NoTestDir", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := baseImageConfig()
		cfg.Items["matrixOS.Root"] = []string{tmpDir}
		runner := testutil.NewMockRunner()
		im := newTestImageWithRunner(cfg, &cds.MockOstree{}, runner)

		err := im.TestImage("/tmp/test.img", "matrixos/amd64/gnome")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("OstreeError", func(t *testing.T) {
		mo := &cds.MockOstree{RemoveFullErr: errors.New("ostree error")}
		im := newTestImage(baseImageConfig(), mo)
		err := im.TestImage("/tmp/x.img", "ref")
		if err == nil {
			t.Error("should propagate ostree error")
		}
	})
}

// --- cleanAndStripRef Tests ---

func TestCleanAndStripRef(t *testing.T) {
	t.Run("WithRemoteAndFull", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		result, err := im.cleanAndStripRef("origin:matrixos/amd64/gnome-full")
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if result != "matrixos/amd64/gnome" {
			t.Errorf("got %q, want matrixos/amd64/gnome", result)
		}
	})

	t.Run("WithoutSuffix", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		result, err := im.cleanAndStripRef("matrixos/amd64/gnome")
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if result != "matrixos/amd64/gnome" {
			t.Errorf("got %q, want matrixos/amd64/gnome", result)
		}
	})

	t.Run("OstreeError", func(t *testing.T) {
		mo := &cds.MockOstree{RemoveFullErr: errors.New("ostree error")}
		im := newTestImage(baseImageConfig(), mo)
		_, err := im.cleanAndStripRef("ref")
		if err == nil {
			t.Error("should propagate ostree error")
		}
	})

	t.Run("EmptyAfterStrip", func(t *testing.T) {
		mo := &cds.MockOstree{RemoveFullResult: "", RemoveFullResultSet: true}
		im := newTestImage(baseImageConfig(), mo)
		_, err := im.cleanAndStripRef("ref")
		if err == nil {
			t.Error("should error for empty result after cleaning")
		}
	})
}

// --- SetupBootloaderConfig Tests ---

func TestSetupBootloaderConfig(t *testing.T) {
	t.Run("EmptyRef", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		err := im.SetupBootloaderConfig("", "/rootfs", "/sysroot", "/boot", "/efiboot", "uuid1", "uuid2")
		if err == nil {
			t.Error("should error for empty ref")
		}
	})

	t.Run("OstreeError", func(t *testing.T) {
		mo := &cds.MockOstree{RemoveFullErr: errors.New("ostree error")}
		im := newTestImage(baseImageConfig(), mo)
		err := im.SetupBootloaderConfig("ref", "/rootfs", "/sysroot", "/boot", "/efiboot", "uuid1", "uuid2")
		if err == nil {
			t.Error("should propagate ostree error")
		}
	})

	t.Run("EmptyOtherParams", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		if err := im.SetupBootloaderConfig("ref", "", "/sysroot", "/boot", "/efi", "u1", "u2"); err == nil {
			t.Error("should error for empty ostreeDeployRootfs")
		}
		if err := im.SetupBootloaderConfig("ref", "/rootfs", "", "/boot", "/efi", "u1", "u2"); err == nil {
			t.Error("should error for empty sysroot")
		}
		if err := im.SetupBootloaderConfig("ref", "/rootfs", "/sys", "", "/efi", "u1", "u2"); err == nil {
			t.Error("should error for empty bootdir")
		}
		if err := im.SetupBootloaderConfig("ref", "/rootfs", "/sys", "/boot", "", "u1", "u2"); err == nil {
			t.Error("should error for empty efibootdir")
		}
		if err := im.SetupBootloaderConfig("ref", "/rootfs", "/sys", "/boot", "/efi", "", "u2"); err == nil {
			t.Error("should error for empty efiUUID")
		}
		if err := im.SetupBootloaderConfig("ref", "/rootfs", "/sys", "/boot", "/efi", "u1", ""); err == nil {
			t.Error("should error for empty bootUUID")
		}
	})
}

// --- SetupVmtestConfig Tests ---

func TestSetupVmtestConfig(t *testing.T) {
	t.Run("EmptyParam", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		err := im.SetupVmtestConfig("")
		if err == nil {
			t.Error("should error for empty bootdir")
		}
	})

	t.Run("NoLoaderConf", func(t *testing.T) {
		tmpDir := t.TempDir()
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		err := im.SetupVmtestConfig(tmpDir)
		if err == nil {
			t.Error("should error when ostree boot config doesn't exist")
		}
	})

	t.Run("Success", func(t *testing.T) {
		tmpDir := t.TempDir()
		loaderDir := filepath.Join(tmpDir, "loader", "entries")
		os.MkdirAll(loaderDir, 0755)
		confContent := "title matrixos\noptions root=UUID=xxx quiet splash rw\n"
		os.WriteFile(filepath.Join(loaderDir, "ostree-1.conf"), []byte(confContent), 0644)

		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		err := im.SetupVmtestConfig(tmpDir)
		if err != nil {
			t.Fatalf("error: %v", err)
		}

		vmtestCfg := filepath.Join(tmpDir, ".imager.vmtest", "entries", "ostree-1.conf")
		data, err := os.ReadFile(vmtestCfg)
		if err != nil {
			t.Fatalf("failed to read vmtest config: %v", err)
		}
		content := string(data)
		if strings.Contains(content, "splash") {
			t.Error("vmtest config should not contain 'splash'")
		}
		if !strings.Contains(content, "console=ttyS0,115200") {
			t.Error("vmtest config should contain console params")
		}
		if !strings.Contains(content, "systemd.log_color=0") {
			t.Error("vmtest config should contain systemd params")
		}
	})
}

// --- InstallSecurebootCerts Tests ---

func TestInstallSecurebootCerts(t *testing.T) {
	t.Run("EmptyParams", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		if err := im.InstallSecurebootCerts("", "/efi", "/efiboot"); err == nil {
			t.Error("should error for empty ostreeDeployRootfs")
		}
		if err := im.InstallSecurebootCerts("/rootfs", "", "/efiboot"); err == nil {
			t.Error("should error for empty mountEfifs")
		}
		if err := im.InstallSecurebootCerts("/rootfs", "/efi", ""); err == nil {
			t.Error("should error for empty efibootdir")
		}
	})

	t.Run("ConfigError", func(t *testing.T) {
		ec := &config.ErrConfig{Err: errors.New("cfg error")}
		im, _ := NewImage(ec, &cds.MockOstree{})
		err := im.InstallSecurebootCerts("/rootfs", "/efi", "/efiboot")
		if err == nil {
			t.Error("should error from broken config")
		}
	})
}

// --- InstallMemtest Tests ---

func TestInstallMemtest(t *testing.T) {
	t.Run("EmptyParams", func(t *testing.T) {
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		if err := im.InstallMemtest("", "/efiboot"); err == nil {
			t.Error("should error for empty ostreeDeployRootfs")
		}
		if err := im.InstallMemtest("/rootfs", ""); err == nil {
			t.Error("should error for empty efibootdir")
		}
	})

	t.Run("NoMemtest", func(t *testing.T) {
		tmpDir := t.TempDir()
		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		err := im.InstallMemtest(tmpDir, filepath.Join(tmpDir, "efiboot"))
		if err != nil {
			t.Fatalf("should not error when memtest not found: %v", err)
		}
	})

	t.Run("Success", func(t *testing.T) {
		tmpDir := t.TempDir()
		memtestDir := filepath.Join(tmpDir, "usr", "share", "memtest86+")
		os.MkdirAll(memtestDir, 0755)
		os.WriteFile(filepath.Join(memtestDir, "memtest.efi64"), []byte("EFI"), 0644)
		efibootdir := filepath.Join(tmpDir, "efiboot")
		os.MkdirAll(efibootdir, 0755)

		im := newTestImage(baseImageConfig(), &cds.MockOstree{})
		err := im.InstallMemtest(tmpDir, efibootdir)
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		copied := filepath.Join(efibootdir, "memtest86plus.efi")
		if _, err := os.Stat(copied); os.IsNotExist(err) {
			t.Error("memtest86plus.efi should have been copied")
		}
	})
}

// --- copyFile Tests ---

func TestCopyFile(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		tmpDir := t.TempDir()
		src := filepath.Join(tmpDir, "src.txt")
		dst := filepath.Join(tmpDir, "dst.txt")
		os.WriteFile(src, []byte("hello world"), 0644)

		err := copyFile(src, dst)
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		data, _ := os.ReadFile(dst)
		if string(data) != "hello world" {
			t.Errorf("got %q, want 'hello world'", string(data))
		}
	})

	t.Run("SrcNotFound", func(t *testing.T) {
		err := copyFile("/nonexistent", "/tmp/dst")
		if err == nil {
			t.Error("should error for nonexistent source")
		}
	})
}
