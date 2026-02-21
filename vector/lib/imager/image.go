package imager

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"matrixos/vector/lib/cds"
	"matrixos/vector/lib/config"
	fslib "matrixos/vector/lib/filesystems"
	"matrixos/vector/lib/runner"
)

// IImage defines the interface for image operations.
// It mirrors all public methods of Image for testability.
type IImage interface {
	// Config accessors
	ImagesOutDir() (string, error)
	MountDir() (string, error)
	ImageSize() (string, error)
	EfiPartitionSize() (string, error)
	BootPartitionSize() (string, error)
	Compressor() (string, error)
	EspPartitionType() (string, error)
	BootPartitionType() (string, error)
	RootPartitionType() (string, error)
	OsName() (string, error)
	BootRoot() (string, error)
	EfiRoot() (string, error)
	RelativeEfiBootPath() (string, error)
	EfiExecutable() (string, error)
	EfiCertificateFileName() (string, error)
	EfiCertificateFileNameDer() (string, error)
	EfiCertificateFileNameKek() (string, error)
	EfiCertificateFileNameKekDer() (string, error)
	ReadOnlyVdb() (string, error)
	DevDir() (string, error)
	LockDir() (string, error)
	LockWaitSeconds() (string, error)
	BuildMetadataFile() (string, error)

	// Operations
	ReleaseVersion(rootfs string) (string, error)
	ImagePath(ref string) (string, error)
	ImagePathWithReleaseVersion(ref, releaseVersion string) (string, error)
	CreateImage(imagePath, imageSize string) error
	ImagePathWithCompressorExtension(imagePath, compressor string) (string, error)
	CompressImage(imagePath, compressor string) error
	BlockDeviceNthPartitionPath(blockDevice string, nth int) (string, error)
	BlockDeviceForPartitionPath(partitionPath string) (string, error)
	PartitionNumber(partitionPath string) (string, error)
	PartitionLabel(partitionPath string) (string, error)
	ClearPartitionTable(devicePath string) error
	GetPartitionType(devicePath string) (string, error)
	DatedFsLabel() string
	PartitionDevices(efiSize, bootSize, imageSize, devicePath string) error
	FormatEfifs(efiDevice string) error
	MountEfifs(efiDevice, mountEfifs string) error
	FormatBootfs(bootDevice string) error
	MountBootfs(bootDevice, mountBootfs string) error
	FormatRootfs(rootDevice string) error
	RootfsKernelArgs() []string
	MountRootfs(rootDevice, mountRootfs string) error
	GetKernelPath(ostreeDeployRootfs string) (string, error)
	SetupPasswords(ostreeDeployRootfs string) error
	SetupBootloaderConfig(ref, ostreeDeployRootfs, sysroot, bootdir, efibootdir, efiUUID, bootUUID string) error
	SetupVmtestConfig(bootdir string) error
	InstallSecurebootCerts(ostreeDeployRootfs, mountEfifs, efibootdir string) error
	InstallMemtest(ostreeDeployRootfs, efibootdir string) error
	GenerateKernelBootArgs(ref, efiDevice, bootDevice, physicalRootDevice, rootDevice string, encryptionEnabled bool) ([]string, error)
	PackageList(rootfs string) ([]string, error)
	SetupHooks(ostreeDeployRootfs, ref string) error
	TestImage(imagePath, ref string) error
	FinalizeFilesystems(mountRootfs, mountBootfs, mountEfifs string) error
	Qcow2ImagePath(imagePath string) (string, error)
	CreateQcow2Image(imagePath string) error
	ShowFinalFilesystemInfo(blockDevice, mountBootfs, mountEfifs string) error
	ShowTestInfo(artifacts []string)
	RemoveImageFile(imagePath string) error
	ImageLockDir() (string, error)
	ImageLockPath(ref string) (string, error)
}

// Image provides image creation and manipulation operations.
type Image struct {
	cfg    config.IConfig
	ostree cds.IOstree
	runner runner.Func
}

// NewImage creates a new Image instance.
func NewImage(cfg config.IConfig, ostree cds.IOstree) (*Image, error) {
	if cfg == nil {
		return nil, errors.New("missing config parameter")
	}
	if ostree == nil {
		return nil, errors.New("missing ostree parameter")
	}
	return &Image{
		cfg:    cfg,
		ostree: ostree,
		runner: runner.Run,
	}, nil
}

// --- Config accessors ---

// ImagesOutDir returns the directory where generated images are stored.
func (im *Image) ImagesOutDir() (string, error) {
	v, err := im.cfg.GetItem("Imager.ImagesDir")
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", errors.New("invalid Imager.ImagesDir")
	}
	return v, nil
}

// MountDir returns the directory where image partitions are mounted.
func (im *Image) MountDir() (string, error) {
	v, err := im.cfg.GetItem("Imager.MountDir")
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", errors.New("invalid Imager.MountDir")
	}
	return v, nil
}

// ImageSize returns the configured image size (e.g. "32G").
func (im *Image) ImageSize() (string, error) {
	v, err := im.cfg.GetItem("Imager.ImageSize")
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", errors.New("invalid Imager.ImageSize")
	}
	return v, nil
}

// EfiPartitionSize returns the configured EFI partition size (e.g. "200M").
func (im *Image) EfiPartitionSize() (string, error) {
	v, err := im.cfg.GetItem("Imager.EfiPartitionSize")
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", errors.New("invalid Imager.EfiPartitionSize")
	}
	return v, nil
}

// BootPartitionSize returns the configured boot partition size (e.g. "1G").
func (im *Image) BootPartitionSize() (string, error) {
	v, err := im.cfg.GetItem("Imager.BootPartitionSize")
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", errors.New("invalid Imager.BootPartitionSize")
	}
	return v, nil
}

// Compressor returns the configured compressor command string (e.g. "xz -f -0 -T0").
func (im *Image) Compressor() (string, error) {
	v, err := im.cfg.GetItem("Imager.Compressor")
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", errors.New("invalid Imager.Compressor")
	}
	return v, nil
}

// EspPartitionType returns the ESP partition type GUID.
func (im *Image) EspPartitionType() (string, error) {
	v, err := im.cfg.GetItem("Imager.EspPartitionType")
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", errors.New("invalid Imager.EspPartitionType")
	}
	return v, nil
}

// BootPartitionType returns the boot partition type GUID.
func (im *Image) BootPartitionType() (string, error) {
	v, err := im.cfg.GetItem("Imager.BootPartitionType")
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", errors.New("invalid Imager.BootPartitionType")
	}
	return v, nil
}

// RootPartitionType returns the root partition type GUID.
func (im *Image) RootPartitionType() (string, error) {
	v, err := im.cfg.GetItem("Imager.RootPartitionType")
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", errors.New("invalid Imager.RootPartitionType")
	}
	return v, nil
}

// OsName returns the OS name.
func (im *Image) OsName() (string, error) {
	v, err := im.cfg.GetItem("matrixOS.OsName")
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", errors.New("invalid matrixOS.OsName")
	}
	return v, nil
}

// BootRoot returns the boot filesystem mount point (e.g. "/boot").
func (im *Image) BootRoot() (string, error) {
	v, err := im.cfg.GetItem("Imager.BootRoot")
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", errors.New("invalid Imager.BootRoot")
	}
	return v, nil
}

// EfiRoot returns the EFI filesystem mount point (e.g. "/efi").
func (im *Image) EfiRoot() (string, error) {
	v, err := im.cfg.GetItem("Imager.EfiRoot")
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", errors.New("invalid Imager.EfiRoot")
	}
	return v, nil
}

// RelativeEfiBootPath returns the path relative to EfiRoot where the standard ESP
// boot directory is (e.g. "efi/BOOT").
func (im *Image) RelativeEfiBootPath() (string, error) {
	v, err := im.cfg.GetItem("Imager.RelativeEfiBootPath")
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", errors.New("invalid Imager.RelativeEfiBootPath")
	}
	return v, nil
}

// EfiExecutable returns the EFI executable name (e.g. "BOOTX64.EFI").
func (im *Image) EfiExecutable() (string, error) {
	v, err := im.cfg.GetItem("Imager.EfiExecutable")
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", errors.New("invalid Imager.EfiExecutable")
	}
	return v, nil
}

// EfiCertificateFileName returns the SecureBoot PEM certificate file name.
func (im *Image) EfiCertificateFileName() (string, error) {
	v, err := im.cfg.GetItem("Imager.EfiCertificateFileName")
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", errors.New("invalid Imager.EfiCertificateFileName")
	}
	return v, nil
}

// EfiCertificateFileNameDer returns the SecureBoot DER certificate file name.
func (im *Image) EfiCertificateFileNameDer() (string, error) {
	v, err := im.cfg.GetItem("Imager.EfiCertificateFileNameDer")
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", errors.New("invalid Imager.EfiCertificateFileNameDer")
	}
	return v, nil
}

// EfiCertificateFileNameKek returns the SecureBoot KEK PEM certificate file name.
func (im *Image) EfiCertificateFileNameKek() (string, error) {
	v, err := im.cfg.GetItem("Imager.EfiCertificateFileNameKek")
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", errors.New("invalid Imager.EfiCertificateFileNameKek")
	}
	return v, nil
}

// EfiCertificateFileNameKekDer returns the SecureBoot KEK DER certificate file name.
func (im *Image) EfiCertificateFileNameKekDer() (string, error) {
	v, err := im.cfg.GetItem("Imager.EfiCertificateFileNameKekDer")
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", errors.New("invalid Imager.EfiCertificateFileNameKekDer")
	}
	return v, nil
}

// ReadOnlyVdb returns the read-only VDB path (e.g. "/usr/var-db-pkg").
func (im *Image) ReadOnlyVdb() (string, error) {
	v, err := im.cfg.GetItem("Releaser.ReadOnlyVdb")
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", errors.New("invalid Releaser.ReadOnlyVdb")
	}
	return v, nil
}

// DevDir returns the matrixOS dev directory (Root).
func (im *Image) DevDir() (string, error) {
	v, err := im.cfg.GetItem("matrixOS.Root")
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", errors.New("invalid matrixOS.Root")
	}
	return v, nil
}

// LockDir returns the configured image lock directory.
func (im *Image) LockDir() (string, error) {
	v, err := im.cfg.GetItem("Imager.LocksDir")
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", errors.New("invalid Imager.LocksDir")
	}
	return v, nil
}

// LockWaitSeconds returns the configured lock wait timeout in seconds.
func (im *Image) LockWaitSeconds() (string, error) {
	v, err := im.cfg.GetItem("Imager.LockWaitSeconds")
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", errors.New("invalid Imager.LockWaitSeconds")
	}
	return v, nil
}

// BuildMetadataFile returns the build metadata file path (combining
// ChrootMetadataDir and ChrootMetadataDirBuildFileName).
func (im *Image) BuildMetadataFile() (string, error) {
	metadataDir, err := im.cfg.GetItem("Seeder.ChrootMetadataDir")
	if err != nil {
		return "", err
	}
	if metadataDir == "" {
		return "", errors.New("invalid Seeder.ChrootMetadataDir")
	}
	buildFileName, err := im.cfg.GetItem("Seeder.ChrootMetadataDirBuildFileName")
	if err != nil {
		return "", err
	}
	if buildFileName == "" {
		return "", errors.New("invalid Seeder.ChrootMetadataDirBuildFileName")
	}
	return filepath.Join(metadataDir, buildFileName), nil
}

// --- Helpers ---

// imagePath builds the full image file path from a suffix.
func (im *Image) imagePath(suffix string) (string, error) {
	outDir, err := im.ImagesOutDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(outDir, suffix), nil
}

// cleanAndStripRef cleans a remote prefix and removes the -full suffix from a ref.
func (im *Image) cleanAndStripRef(ref string) (string, error) {
	ref = cds.CleanRemoteFromRef(ref)
	stripped, err := im.ostree.RemoveFullFromBranch(ref)
	if err != nil {
		return "", err
	}
	if stripped == "" {
		return "", errors.New("invalid ref parameter after cleaning")
	}
	return stripped, nil
}

// refToSuffix converts slashes in a ref to underscores for use in file names.
func refToSuffix(ref string) string {
	return strings.ReplaceAll(ref, "/", "_")
}

// --- Operations ---

// ReleaseVersion extracts or generates a release version string for an image.
// It attempts to read a build metadata file from the rootfs for the version;
// if unavailable, falls back to the current date (YYYYMMDD).
func (im *Image) ReleaseVersion(rootfs string) (string, error) {
	if rootfs == "" {
		return "", errors.New("missing rootfs parameter")
	}

	releaseVersion := time.Now().Format("20060102")

	metadataRelPath, err := im.BuildMetadataFile()
	if err != nil {
		return "", fmt.Errorf("failed to determine build metadata file path: %w", err)
	}
	metadataFile := filepath.Join(rootfs, metadataRelPath)

	if fslib.FileExists(metadataFile) {
		fmt.Fprintf(os.Stderr, "Build metadata:\n")
		data, err := os.ReadFile(metadataFile)
		if err != nil {
			return "", fmt.Errorf("failed to read build metadata file %s: %w", metadataFile, err)
		}
		fmt.Fprint(os.Stderr, string(data))

		// Extract version from SEED_NAME= line.
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "SEED_NAME=") {
				seedName := strings.TrimPrefix(line, "SEED_NAME=")
				// Version is the part after the last '-'.
				if idx := strings.LastIndex(seedName, "-"); idx >= 0 {
					releaseVersion = seedName[idx+1:]
					fmt.Fprintf(os.Stderr, "Extracted release version: %s\n", releaseVersion)
				} else {
					fmt.Fprintf(os.Stderr, "WARNING: SEED_NAME= value has no '-' separator\n")
				}
				break
			}
		}
		if scanner.Err() != nil {
			return "", fmt.Errorf("failed to scan build metadata file: %w", scanner.Err())
		}
	} else {
		fmt.Fprintf(os.Stderr, "WARNING! Build metadata file not found: %s\n", metadataFile)
	}

	return releaseVersion, nil
}

// ImagePath returns the image file path for a given ostree ref.
func (im *Image) ImagePath(ref string) (string, error) {
	if ref == "" {
		return "", errors.New("missing ref parameter")
	}
	ref = cds.CleanRemoteFromRef(ref)
	suffix := refToSuffix(ref) + ".img"
	return im.imagePath(suffix)
}

// ImagePathWithReleaseVersion returns the image file path with an embedded release version.
func (im *Image) ImagePathWithReleaseVersion(ref, releaseVersion string) (string, error) {
	if ref == "" {
		return "", errors.New("missing ref parameter")
	}
	if releaseVersion == "" {
		return "", errors.New("missing releaseVersion parameter")
	}
	ref = cds.CleanRemoteFromRef(ref)
	suffix := refToSuffix(ref) + "-" + releaseVersion + ".img"
	return im.imagePath(suffix)
}

// CreateImage creates a sparse image file at imagePath with the given size.
func (im *Image) CreateImage(imagePath, imageSize string) (retErr error) {
	if imagePath == "" {
		return errors.New("missing imagePath parameter")
	}
	if imageSize == "" {
		return errors.New("missing imageSize parameter")
	}

	imagesDir := filepath.Dir(imagePath)
	fmt.Fprintf(os.Stdout, "Creating images directory: %s (if it does not exist)\n", imagesDir)
	if err := os.MkdirAll(imagesDir, 0755); err != nil {
		return fmt.Errorf("failed to create images directory %s: %w", imagesDir, err)
	}

	// Don't skip removing or sgdisk gets confused due to truncate.
	if err := im.RemoveImageFile(imagePath); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "Creating block device image file: %s\n", imagePath)
	return im.runner(nil, os.Stdout, os.Stderr, "truncate", "-s", imageSize, imagePath)
}

// ImagePathWithCompressorExtension appends the compressor's file extension to the image path.
// The extension is derived from the first word of the compressor command string.
func (im *Image) ImagePathWithCompressorExtension(imagePath, compressor string) (string, error) {
	if imagePath == "" {
		return "", errors.New("missing imagePath parameter")
	}
	if compressor == "" {
		return "", errors.New("missing compressor parameter")
	}
	parts := strings.Fields(compressor)
	return imagePath + "." + parts[0], nil
}

// CompressImage compresses an image file using the configured compressor.
func (im *Image) CompressImage(imagePath, compressor string) error {
	if imagePath == "" {
		return errors.New("missing imagePath parameter")
	}
	if compressor == "" {
		return errors.New("missing compressor parameter")
	}

	imagePathWithExt, err := im.ImagePathWithCompressorExtension(imagePath, compressor)
	if err != nil {
		return err
	}

	parts := strings.Fields(compressor)
	args := append(parts[1:], imagePath)
	if err := im.runner(nil, os.Stdout, os.Stderr, parts[0], args...); err != nil {
		return fmt.Errorf("compression failed: %w", err)
	}

	if !fslib.FileExists(imagePathWithExt) {
		return fmt.Errorf("compressed image was not created at the expected path: %s", imagePathWithExt)
	}
	return nil
}

// BlockDeviceNthPartitionPath returns the path of the nth partition of a block device.
func (im *Image) BlockDeviceNthPartitionPath(blockDevice string, nth int) (string, error) {
	if blockDevice == "" {
		return "", errors.New("missing blockDevice parameter")
	}
	if nth <= 0 {
		return "", errors.New("invalid nth parameter")
	}

	cmd := exec.Command("lsblk", "-nr", "-o", "PATH,PARTN", blockDevice)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("lsblk failed for %s: %w", blockDevice, err)
	}

	nthStr := fmt.Sprintf("%d", nth)
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[1] == nthStr {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("partition %d not found on %s", nth, blockDevice)
}

// BlockDeviceForPartitionPath returns the parent block device for a partition path.
func (im *Image) BlockDeviceForPartitionPath(partitionPath string) (string, error) {
	if partitionPath == "" {
		return "", errors.New("missing partitionPath parameter")
	}
	cmd := exec.Command("lsblk", "-no", "PKNAME", "-p", partitionPath)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("lsblk failed for %s: %w", partitionPath, err)
	}
	result := strings.TrimSpace(string(out))
	if result == "" {
		return "", fmt.Errorf("no parent block device found for %s", partitionPath)
	}
	return result, nil
}

// PartitionNumber returns the partition number of a partition path.
func (im *Image) PartitionNumber(partitionPath string) (string, error) {
	if partitionPath == "" {
		return "", errors.New("missing partitionPath parameter")
	}
	cmd := exec.Command("lsblk", "-no", "PARTN", "-p", partitionPath)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("lsblk failed for %s: %w", partitionPath, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// PartitionLabel returns the label of a partition.
func (im *Image) PartitionLabel(partitionPath string) (string, error) {
	if partitionPath == "" {
		return "", errors.New("missing partitionPath parameter")
	}
	cmd := exec.Command("lsblk", "-no", "LABEL", "-p", partitionPath)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("lsblk failed for %s: %w", partitionPath, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ClearPartitionTable clears the partition table on a device using sgdisk.
func (im *Image) ClearPartitionTable(devicePath string) error {
	if devicePath == "" {
		return errors.New("missing devicePath parameter")
	}

	fmt.Fprintf(os.Stdout, "Clearing partition table on %s ...\n", devicePath)
	if err := im.runner(nil, os.Stdout, os.Stderr, "sgdisk", "-g", "-o", devicePath); err != nil {
		return fmt.Errorf("sgdisk -g -o failed on %s: %w", devicePath, err)
	}
	return im.runner(nil, os.Stdout, os.Stderr, "sgdisk", "-Z", devicePath)
}

// GetPartitionType returns the partition type GUID (uppercased) for a device.
func (im *Image) GetPartitionType(devicePath string) (string, error) {
	if devicePath == "" {
		return "", errors.New("missing devicePath parameter")
	}
	cmd := exec.Command("lsblk", "-no", "PARTTYPE", devicePath)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("lsblk failed for %s: %w", devicePath, err)
	}
	return strings.ToUpper(strings.TrimSpace(string(out))), nil
}

// DatedFsLabel returns a filesystem label based on the current date (YYYYMMDD).
func (im *Image) DatedFsLabel() string {
	return time.Now().Format("20060102")
}

// PartitionDevices creates the EFI, boot, and root partitions on a device.
func (im *Image) PartitionDevices(efiSize, bootSize, imageSize, devicePath string) error {
	if efiSize == "" {
		return errors.New("missing efiSize parameter")
	}
	if bootSize == "" {
		return errors.New("missing bootSize parameter")
	}
	if imageSize == "" {
		return errors.New("missing imageSize parameter")
	}
	if devicePath == "" {
		return errors.New("missing devicePath parameter")
	}

	espPartType, err := im.EspPartitionType()
	if err != nil {
		return err
	}
	bootPartType, err := im.BootPartitionType()
	if err != nil {
		return err
	}
	rootPartType, err := im.RootPartitionType()
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "Partitioning %s:\n", devicePath)
	fmt.Fprintf(os.Stdout, " --> p1 (EFI: %s)\n", efiSize)
	fmt.Fprintf(os.Stdout, " --> p2 (BOOT: %s)\n", bootSize)
	fmt.Fprintf(os.Stdout, " --> p3 (ROOT: Remainder of %s, plus autogrow)\n\n", imageSize)

	// Create EFI partition.
	if err := im.runner(nil, os.Stdout, os.Stderr, "sgdisk",
		"-n", fmt.Sprintf("1:0:+%s", efiSize),
		"-t", fmt.Sprintf("1:%s", espPartType),
		devicePath); err != nil {
		return fmt.Errorf("sgdisk EFI partition failed: %w", err)
	}

	// Create boot partition.
	if err := im.runner(nil, os.Stdout, os.Stderr, "sgdisk",
		"-n", fmt.Sprintf("2:0:+%s", bootSize),
		"-t", fmt.Sprintf("2:%s", bootPartType),
		devicePath); err != nil {
		return fmt.Errorf("sgdisk boot partition failed: %w", err)
	}

	// Create root partition with -10M padding for systemd-repart.
	if err := im.runner(nil, os.Stdout, os.Stderr, "sgdisk",
		"-n", "3:0:-10M",
		"-t", fmt.Sprintf("3:%s", rootPartType),
		devicePath); err != nil {
		return fmt.Errorf("sgdisk root partition failed: %w", err)
	}

	// Set the auto-grow flag (bit 59) on partition 3.
	if err := im.runner(nil, os.Stdout, os.Stderr, "sgdisk",
		"-A", "3:set:59",
		devicePath); err != nil {
		return fmt.Errorf("sgdisk set auto-grow flag failed: %w", err)
	}

	fmt.Fprintln(os.Stdout, "Refreshing partition table ...")
	if err := im.runner(nil, os.Stdout, os.Stderr, "partprobe", "-s", devicePath); err != nil {
		return fmt.Errorf("partprobe failed: %w", err)
	}

	fslib.DevicesSettle()
	return nil
}

// FormatEfifs creates a FAT32 filesystem on the EFI partition.
func (im *Image) FormatEfifs(efiDevice string) error {
	if efiDevice == "" {
		return errors.New("missing efiDevice parameter")
	}

	fmt.Fprintf(os.Stdout, "Creating EFI partition on %s\n", efiDevice)
	label := "ME" + im.DatedFsLabel()
	return im.runner(nil, os.Stdout, os.Stderr, "mkfs.vfat", "-F", "32", "-n", label, efiDevice)
}

// MountEfifs mounts the EFI partition.
func (im *Image) MountEfifs(efiDevice, mountEfifs string) error {
	if efiDevice == "" {
		return errors.New("missing efiDevice parameter")
	}
	if mountEfifs == "" {
		return errors.New("missing mountEfifs parameter")
	}

	if !fslib.DirectoryExists(mountEfifs) {
		fmt.Fprintf(os.Stdout, "Creating %s ...\n", mountEfifs)
		if err := os.MkdirAll(mountEfifs, 0755); err != nil {
			return fmt.Errorf("failed to create mount point %s: %w", mountEfifs, err)
		}
	}

	fmt.Fprintf(os.Stdout, "Mounting %s to %s\n", efiDevice, mountEfifs)
	return im.runner(nil, os.Stdout, os.Stderr, "mount", "-t", "vfat", efiDevice, mountEfifs)
}

// FormatBootfs creates a btrfs filesystem on the boot partition.
func (im *Image) FormatBootfs(bootDevice string) error {
	if bootDevice == "" {
		return errors.New("missing bootDevice parameter")
	}

	label := "MB" + im.DatedFsLabel()
	fmt.Fprintf(os.Stdout, "Creating btrfs on %s (boot)\n", bootDevice)
	return im.runner(nil, os.Stdout, os.Stderr, "mkfs.btrfs", "-f", "-L", label, bootDevice)
}

// MountBootfs mounts the boot partition.
func (im *Image) MountBootfs(bootDevice, mountBootfs string) error {
	if bootDevice == "" {
		return errors.New("missing bootDevice parameter")
	}
	if mountBootfs == "" {
		return errors.New("missing mountBootfs parameter")
	}

	if !fslib.DirectoryExists(mountBootfs) {
		fmt.Fprintf(os.Stdout, "Creating %s ...\n", mountBootfs)
		if err := os.MkdirAll(mountBootfs, 0755); err != nil {
			return fmt.Errorf("failed to create mount point %s: %w", mountBootfs, err)
		}
	}

	fmt.Fprintf(os.Stdout, "Mounting %s to %s\n", bootDevice, mountBootfs)
	return im.runner(nil, os.Stdout, os.Stderr, "mount", bootDevice, mountBootfs)
}

// FormatRootfs creates a btrfs filesystem on the root partition.
func (im *Image) FormatRootfs(rootDevice string) error {
	if rootDevice == "" {
		return errors.New("missing rootDevice parameter")
	}

	label := "MR" + im.DatedFsLabel()
	fmt.Fprintf(os.Stdout, "Creating btrfs on %s (root)\n", rootDevice)
	return im.runner(nil, os.Stdout, os.Stderr, "mkfs.btrfs", "-f", "-L", label, rootDevice)
}

// RootfsKernelArgs returns the default kernel arguments for the root filesystem.
func (im *Image) RootfsKernelArgs() []string {
	return []string{"rootflags=discard=async"}
}

// MountRootfs mounts the root partition with btrfs compression options.
func (im *Image) MountRootfs(rootDevice, mountRootfs string) error {
	if rootDevice == "" {
		return errors.New("missing rootDevice parameter")
	}
	if mountRootfs == "" {
		return errors.New("missing mountRootfs parameter")
	}

	compression := "zstd:6"
	btrfsOpts := fmt.Sprintf("compress-force=%s,space_cache=v2,commit=120", compression)
	fmt.Fprintf(os.Stdout, "Mounting %s to %s\n", rootDevice, mountRootfs)
	return im.runner(nil, os.Stdout, os.Stderr, "mount", "-o", btrfsOpts, rootDevice, mountRootfs)
}

// GetKernelPath returns the kernel version directory name from the deployed rootfs.
func (im *Image) GetKernelPath(ostreeDeployRootfs string) (string, error) {
	if ostreeDeployRootfs == "" {
		return "", errors.New("missing ostreeDeployRootfs parameter")
	}

	modulesDir := filepath.Join(ostreeDeployRootfs, "usr", "lib", "modules")
	entries, err := os.ReadDir(modulesDir)
	if err != nil {
		return "", fmt.Errorf("failed to read modules directory %s: %w", modulesDir, err)
	}

	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) == 0 {
		return "", fmt.Errorf("no kernel directory found in %s", modulesDir)
	}
	sort.Strings(dirs)
	return dirs[0], nil
}

// SetupPasswords sets default passwords for the matrix and root users.
func (im *Image) SetupPasswords(ostreeDeployRootfs string) error {
	if ostreeDeployRootfs == "" {
		return errors.New("missing ostreeDeployRootfs parameter")
	}

	shadowFile := filepath.Join(ostreeDeployRootfs, "etc", "shadow")

	cmd := exec.Command("openssl", "passwd", "-6", "matrix")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("openssl passwd failed: %w", err)
	}
	passHash := strings.TrimSpace(string(out))
	lastChange := fmt.Sprintf("%d", time.Now().Unix()/86400)

	data, err := os.ReadFile(shadowFile)
	if err != nil {
		return fmt.Errorf("failed to read shadow file: %w", err)
	}

	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		// Remove existing matrix: and root: lines.
		if strings.HasPrefix(line, "matrix:") || strings.HasPrefix(line, "root:") {
			continue
		}
		lines = append(lines, line)
	}

	shadowEntry := func(user string) string {
		return fmt.Sprintf("%s:%s:%s:0:99999:7:::", user, passHash, lastChange)
	}

	fmt.Fprintln(os.Stdout, "Setting the default password of matrix to matrix ...")
	lines = append(lines, shadowEntry("matrix"))
	fmt.Fprintln(os.Stdout, "Setting the default password of root to matrix ...")
	lines = append(lines, shadowEntry("root"))

	return os.WriteFile(shadowFile, []byte(strings.Join(lines, "\n")+"\n"), 0640)
}

// SetupBootloaderConfig sets up the GRUB bootloader configuration.
func (im *Image) SetupBootloaderConfig(ref, ostreeDeployRootfs, sysroot, bootdir, efibootdir, efiUUID, bootUUID string) error {
	if ref == "" {
		return errors.New("missing ref parameter")
	}
	ref, err := im.cleanAndStripRef(ref)
	if err != nil {
		return fmt.Errorf("failed to clean ref: %w", err)
	}
	if ostreeDeployRootfs == "" {
		return errors.New("missing ostreeDeployRootfs parameter")
	}
	if sysroot == "" {
		return errors.New("missing sysroot parameter")
	}
	if bootdir == "" {
		return errors.New("missing bootdir parameter")
	}
	if efibootdir == "" {
		return errors.New("missing efibootdir parameter")
	}
	if efiUUID == "" {
		return errors.New("missing efiUUID parameter")
	}
	if bootUUID == "" {
		return errors.New("missing bootUUID parameter")
	}

	// Verify kernel exists.
	if _, err := im.GetKernelPath(ostreeDeployRootfs); err != nil {
		return fmt.Errorf("failed to determine kernel version: %w", err)
	}

	// Get the boot commit.
	bootCommit, err := im.ostree.BootCommit(sysroot)
	if err != nil || bootCommit == "" {
		return fmt.Errorf("cannot determine ostree boot commit: %w", err)
	}
	fmt.Fprintf(os.Stdout, "Found boot commit: %s\n", bootCommit)

	devDir, err := im.DevDir()
	if err != nil {
		return err
	}

	srcGrubCfg := filepath.Join(devDir, "image", "boot", ref, "grub.cfg")
	resolved, err := filepath.EvalSymlinks(srcGrubCfg)
	if err != nil {
		return fmt.Errorf("failed to resolve grub config path %s: %w", srcGrubCfg, err)
	}
	srcGrubCfg = resolved

	if !fslib.FileExists(srcGrubCfg) {
		return fmt.Errorf("grub config %s does not exist", srcGrubCfg)
	}
	fmt.Fprintf(os.Stdout, "Using grub config from %s\n", srcGrubCfg)

	// Ensure efibootdir exists.
	if err := os.MkdirAll(efibootdir, 0755); err != nil {
		return fmt.Errorf("failed to create efibootdir %s: %w", efibootdir, err)
	}

	dstGrubCfg := filepath.Join(efibootdir, "grub.cfg")
	fmt.Fprintf(os.Stdout, "Copying grub: %s -> %s\n", srcGrubCfg, dstGrubCfg)
	if err := copyFile(srcGrubCfg, dstGrubCfg); err != nil {
		return fmt.Errorf("failed to copy grub config: %w", err)
	}

	// Copy GRUB themes if available.
	osName, err := im.OsName()
	if err != nil {
		return err
	}
	themesDir := filepath.Join(ostreeDeployRootfs, "usr", "share", "grub", "themes", osName+"-theme")
	if fslib.DirectoryExists(themesDir) {
		fmt.Fprintf(os.Stdout, "Copying GRUB themes from %s ...\n", themesDir)
		dstThemesDir := filepath.Join(bootdir, "grub", "themes")
		if err := os.MkdirAll(dstThemesDir, 0755); err != nil {
			return fmt.Errorf("failed to create themes dir: %w", err)
		}
		if err := im.runner(nil, os.Stdout, os.Stderr, "cp", "-v", "-rp", themesDir, dstThemesDir+"/"); err != nil {
			return fmt.Errorf("failed to copy themes: %w", err)
		}
	}

	// Write GRUB_CFG environment file.
	efiRoot, err := im.EfiRoot()
	if err != nil {
		return err
	}
	relEfiBootPath, err := im.RelativeEfiBootPath()
	if err != nil {
		return err
	}
	envDir := filepath.Join(ostreeDeployRootfs, "etc", "environment.d")
	if err := os.MkdirAll(envDir, 0755); err != nil {
		return fmt.Errorf("failed to create environment.d dir: %w", err)
	}
	grubCfgEnv := fmt.Sprintf("GRUB_CFG=%s/%s/grub.cfg\n", efiRoot, relEfiBootPath)
	if err := os.WriteFile(filepath.Join(envDir, "99-matrixos-imager-grub.conf"), []byte(grubCfgEnv), 0644); err != nil {
		return fmt.Errorf("failed to write grub env config: %w", err)
	}

	// Perform template substitutions in grub.cfg.
	grubData, err := os.ReadFile(dstGrubCfg)
	if err != nil {
		return fmt.Errorf("failed to read grub config for substitution: %w", err)
	}
	grubContent := string(grubData)
	grubContent = strings.ReplaceAll(grubContent, "%BOOTUUID%", bootUUID)
	grubContent = strings.ReplaceAll(grubContent, "%EFIUUID%", efiUUID)
	grubContent = strings.ReplaceAll(grubContent, "%OSNAME%", osName)
	if err := os.WriteFile(dstGrubCfg, []byte(grubContent), 0644); err != nil {
		return fmt.Errorf("failed to write substituted grub config: %w", err)
	}

	fmt.Fprintln(os.Stdout, "Current grub.cfg:")
	fmt.Fprintln(os.Stdout, grubContent)
	fmt.Fprintln(os.Stdout, "EOF")

	return nil
}

// SetupVmtestConfig creates a VM test grub config based on the ostree boot config.
func (im *Image) SetupVmtestConfig(bootdir string) error {
	if bootdir == "" {
		return errors.New("missing bootdir parameter")
	}

	fmt.Fprintf(os.Stdout, "Setting up vmtest grub config based on the ostree boot config in %s ...\n", bootdir)

	ostreeBootCfg := filepath.Join(bootdir, "loader", "entries", "ostree-1.conf")
	if !fslib.FileExists(ostreeBootCfg) {
		return fmt.Errorf("%s does not exist, cannot set up vmtest config", ostreeBootCfg)
	}

	vmtestCfgDir := filepath.Join(bootdir, ".imager.vmtest", "entries")
	if err := os.MkdirAll(vmtestCfgDir, 0755); err != nil {
		return fmt.Errorf("failed to create vmtest config dir: %w", err)
	}

	vmtestBootCfg := filepath.Join(vmtestCfgDir, "ostree-1.conf")

	consoleParams := "console=ttyS0,115200"
	systemdParams := "systemd.log_color=0"
	envParams := "systemd.setenv=SYSTEMD_COLORS=0 systemd.setenv=SYSTEMD_URLIFY=0"
	bootParams := consoleParams + " " + systemdParams + " " + envParams

	if err := copyFile(ostreeBootCfg, vmtestBootCfg); err != nil {
		return fmt.Errorf("failed to copy vmtest config: %w", err)
	}

	data, err := os.ReadFile(vmtestBootCfg)
	if err != nil {
		return fmt.Errorf("failed to read vmtest config: %w", err)
	}

	content := string(data)
	content = strings.ReplaceAll(content, "splash", "")
	content = strings.ReplaceAll(content, "quiet", bootParams)

	if err := os.WriteFile(vmtestBootCfg, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write vmtest config: %w", err)
	}

	fmt.Fprintf(os.Stdout, "Set up vmtest grub config at %s\n", vmtestBootCfg)
	fmt.Fprintln(os.Stdout, "Current vmtest grub config:")
	fmt.Fprintln(os.Stdout, content)
	fmt.Fprintln(os.Stdout, "EOF")

	return nil
}

// InstallSecurebootCerts installs SecureBoot certificates on the EFI partition.
func (im *Image) InstallSecurebootCerts(ostreeDeployRootfs, mountEfifs, efibootdir string) error {
	if ostreeDeployRootfs == "" {
		return errors.New("missing ostreeDeployRootfs parameter")
	}
	if mountEfifs == "" {
		return errors.New("missing mountEfifs parameter")
	}
	if efibootdir == "" {
		return errors.New("missing efibootdir parameter")
	}

	certFileName, err := im.EfiCertificateFileName()
	if err != nil {
		return err
	}
	certDerFileName, err := im.EfiCertificateFileNameDer()
	if err != nil {
		return err
	}
	kekFileName, err := im.EfiCertificateFileNameKek()
	if err != nil {
		return err
	}
	kekDerFileName, err := im.EfiCertificateFileNameKekDer()
	if err != nil {
		return err
	}

	// SecureBoot certificate (db).
	sbCert := filepath.Join(ostreeDeployRootfs, "etc", "portage", "secureboot.pem")
	if fslib.FileExists(sbCert) {
		fmt.Fprintln(os.Stdout, "Copying SecureBoot cert to EFI partition ...")
		if err := copyFile(sbCert, filepath.Join(mountEfifs, certFileName)); err != nil {
			return fmt.Errorf("failed to copy SecureBoot cert: %w", err)
		}

		fmt.Fprintln(os.Stdout, "Generating SecureBoot MOK ...")
		if err := im.runner(nil, os.Stdout, os.Stderr,
			"openssl", "x509", "-in", sbCert,
			"-outform", "DER", "-out", filepath.Join(mountEfifs, certDerFileName)); err != nil {
			return fmt.Errorf("openssl DER conversion failed: %w", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "NO SECUREBOOT CERT AT: %s -- ignoring.\n", sbCert)
	}

	// SecureBoot KEK certificate.
	sbKek := filepath.Join(ostreeDeployRootfs, "etc", "portage", "secureboot-kek.pem")
	if fslib.FileExists(sbKek) {
		fmt.Fprintln(os.Stdout, "Copying SecureBoot KEK cert to EFI partition ...")
		if err := copyFile(sbKek, filepath.Join(mountEfifs, kekFileName)); err != nil {
			return fmt.Errorf("failed to copy SecureBoot KEK cert: %w", err)
		}

		fmt.Fprintln(os.Stdout, "Generating SecureBoot KEK DER for convenience ...")
		if err := im.runner(nil, os.Stdout, os.Stderr,
			"openssl", "x509", "-in", sbKek,
			"-outform", "DER", "-out", filepath.Join(mountEfifs, kekDerFileName)); err != nil {
			return fmt.Errorf("openssl KEK DER conversion failed: %w", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "NO SECUREBOOT CERT AT: %s -- ignoring.\n", sbKek)
	}

	// Copy the shim binaries.
	shimDir := filepath.Join(ostreeDeployRootfs, "usr", "share", "shim")
	fmt.Fprintf(os.Stdout, "Copying shim for Secureboot from %s to %s ...\n", shimDir, efibootdir)
	return im.runner(nil, os.Stdout, os.Stderr, "cp", "-v", shimDir+"/.", efibootdir+"/")
}

// InstallMemtest installs the memtest86+ EFI binary to the EFI boot directory.
func (im *Image) InstallMemtest(ostreeDeployRootfs, efibootdir string) error {
	if ostreeDeployRootfs == "" {
		return errors.New("missing ostreeDeployRootfs parameter")
	}
	if efibootdir == "" {
		return errors.New("missing efibootdir parameter")
	}

	memtestBin := filepath.Join(ostreeDeployRootfs, "usr", "share", "memtest86+", "memtest.efi64")
	if !fslib.PathExists(memtestBin) {
		fmt.Fprintf(os.Stderr, "WARNING: %s not available, please install memtest86+\n", memtestBin)
		return nil
	}
	return copyFile(memtestBin, filepath.Join(efibootdir, "memtest86plus.efi"))
}

// GenerateKernelBootArgs generates the kernel boot arguments for the image.
func (im *Image) GenerateKernelBootArgs(ref, efiDevice, bootDevice, physicalRootDevice, rootDevice string, encryptionEnabled bool) ([]string, error) {
	ref, err := im.cleanAndStripRef(ref)
	if err != nil {
		return nil, fmt.Errorf("failed to clean ref: %w", err)
	}
	if efiDevice == "" {
		return nil, errors.New("missing efiDevice parameter")
	}
	if bootDevice == "" {
		return nil, errors.New("missing bootDevice parameter")
	}
	if physicalRootDevice == "" {
		return nil, errors.New("missing physicalRootDevice parameter")
	}
	if rootDevice == "" {
		return nil, errors.New("missing rootDevice parameter")
	}

	bootArgs := im.RootfsKernelArgs()

	// Root device UUID for LUKS.
	rootDeviceUUID, err := fslib.DeviceUUID(physicalRootDevice)
	if err != nil {
		return nil, fmt.Errorf("unable to get device UUID for %s: %w", physicalRootDevice, err)
	}
	if encryptionEnabled {
		bootArgs = append(bootArgs, fmt.Sprintf("rd.luks.uuid=%s", rootDeviceUUID))
	}

	// EFI partition mount via systemd.
	efiRoot, err := im.EfiRoot()
	if err != nil {
		return nil, err
	}
	efiPartUUID, err := fslib.DevicePartUUID(efiDevice)
	if err != nil {
		return nil, fmt.Errorf("unable to get PARTUUID of EFI partition: %w", err)
	}
	bootArgs = append(bootArgs, fmt.Sprintf("systemd.mount-extra=PARTUUID=%s:%s:auto:defaults", efiPartUUID, efiRoot))

	// Boot partition mount via systemd.
	bootRoot, err := im.BootRoot()
	if err != nil {
		return nil, err
	}
	bootPartUUID, err := fslib.DevicePartUUID(bootDevice)
	if err != nil {
		return nil, fmt.Errorf("unable to get PARTUUID of boot partition: %w", err)
	}
	bootArgs = append(bootArgs, fmt.Sprintf("systemd.mount-extra=PARTUUID=%s:%s:auto:defaults", bootPartUUID, bootRoot))

	// Read additional kernel cmdline params from the image boot directory.
	devDir, err := im.DevDir()
	if err != nil {
		return nil, err
	}
	cmdlineFile := filepath.Join(devDir, "image", "boot", ref, "cmdline.conf")
	if fslib.FileExists(cmdlineFile) {
		fmt.Fprintf(os.Stdout, "Reading additional kernel cmdline params from %s ...\n", cmdlineFile)
		data, err := os.ReadFile(cmdlineFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read cmdline file: %w", err)
		}
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			bootArgs = append(bootArgs, line)
		}
	} else {
		fmt.Fprintf(os.Stderr, "WARNING: no additional kernel cmdline params available, %s does not exist.\n", cmdlineFile)
	}

	return bootArgs, nil
}

// PackageList returns the list of packages installed in a rootfs.
func (im *Image) PackageList(rootfs string) ([]string, error) {
	if rootfs == "" {
		return nil, errors.New("missing rootfs parameter")
	}

	roVdb, err := im.ReadOnlyVdb()
	if err != nil {
		return nil, err
	}

	vdb := filepath.Join(strings.TrimRight(rootfs, "/"), roVdb)
	if !fslib.DirectoryExists(vdb) {
		fmt.Fprintf(os.Stderr, "%s does not exist. cannot generate pkglist\n", vdb)
		return nil, nil
	}

	var pkgList []string
	categories, err := os.ReadDir(vdb)
	if err != nil {
		return nil, fmt.Errorf("failed to read vdb directory %s: %w", vdb, err)
	}
	for _, cat := range categories {
		if !cat.IsDir() {
			continue
		}
		catPath := filepath.Join(vdb, cat.Name())
		pkgs, err := os.ReadDir(catPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read category directory %s: %w", catPath, err)
		}
		for _, pkg := range pkgs {
			pkgList = append(pkgList, filepath.Join(cat.Name(), pkg.Name()))
		}
	}

	fmt.Fprintln(os.Stdout, "Generated package list:")
	for _, pkg := range pkgList {
		fmt.Fprintf(os.Stdout, ">> %s\n", pkg)
	}
	return pkgList, nil
}

// SetupHooks runs image-specific hook scripts.
func (im *Image) SetupHooks(ostreeDeployRootfs, ref string) error {
	if ostreeDeployRootfs == "" {
		return errors.New("missing ostreeDeployRootfs parameter")
	}
	if ref == "" {
		return errors.New("missing ref parameter")
	}

	ref, err := im.cleanAndStripRef(ref)
	if err != nil {
		return fmt.Errorf("failed to clean ref: %w", err)
	}

	devDir, err := im.DevDir()
	if err != nil {
		return err
	}

	hooksSrcDir := filepath.Join(devDir, "image", "hooks")
	if !fslib.DirectoryExists(hooksSrcDir) {
		fmt.Fprintf(os.Stderr, "hooks source dir %s does not exist\n", hooksSrcDir)
		return nil
	}

	hookExec := filepath.Join(hooksSrcDir, ref+".sh")
	if !fslib.FileExists(hookExec) {
		fmt.Fprintf(os.Stderr, "hook script %s does not exist\n", hookExec)
		return nil
	}

	info, err := os.Stat(hookExec)
	if err != nil {
		return fmt.Errorf("failed to stat hook script: %w", err)
	}
	if info.Mode()&0111 == 0 {
		return fmt.Errorf("hook script %s is not executable", hookExec)
	}

	cmd := exec.Command(hookExec)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"MATRIXOS_DEV_DIR="+devDir,
		"ROOTFS="+ostreeDeployRootfs,
		"REF="+ref,
	)
	return cmd.Run()
}

// TestImage copies an image to a temp directory and runs test scripts against it.
func (im *Image) TestImage(imagePath, ref string) error {
	if imagePath == "" {
		return errors.New("missing imagePath parameter")
	}
	if ref == "" {
		return errors.New("missing ref parameter")
	}

	ref, err := im.cleanAndStripRef(ref)
	if err != nil {
		return fmt.Errorf("failed to clean ref: %w", err)
	}

	devDir, err := im.DevDir()
	if err != nil {
		return err
	}

	testDir := filepath.Join(devDir, "image", "tests", ref)
	if !fslib.DirectoryExists(testDir) {
		fmt.Fprintf(os.Stderr, "test dir %s does not exist, skipping test\n", testDir)
		return nil
	}

	mountDir, err := im.MountDir()
	if err != nil {
		return err
	}

	imageTempDir, err := fslib.CreateTempDir(mountDir, refToSuffix(ref))
	if err != nil {
		return fmt.Errorf("failed to create temp dir for testing: %w", err)
	}
	defer os.RemoveAll(imageTempDir)

	imageName := filepath.Base(imagePath)
	testImagePath := filepath.Join(imageTempDir, imageName)
	fmt.Fprintf(os.Stdout, "Copying image to %s for testing ...\n", testImagePath)
	if err := im.runner(nil, os.Stdout, os.Stderr, "cp", "--reflink=auto", "-v", imagePath, testImagePath); err != nil {
		return fmt.Errorf("failed to copy image for testing: %w", err)
	}

	logsDir, err := im.cfg.GetItem("matrixOS.LogsDir")
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(testDir)
	if err != nil {
		return fmt.Errorf("failed to read test dir: %w", err)
	}
	for _, entry := range entries {
		ts := filepath.Join(testDir, entry.Name())
		info, err := os.Stat(ts)
		if err != nil {
			continue
		}
		if !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
			fmt.Fprintf(os.Stderr, "Skipping non-executable test script %s\n", ts)
			continue
		}

		fmt.Fprintf(os.Stdout, "Running test script %s ...\n", ts)
		cmd := exec.Command(ts)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = append(os.Environ(),
			"MATRIXOS_DEV_DIR="+devDir,
			"MATRIXOS_LOGS_DIR="+logsDir,
			"IMAGE_PATH="+testImagePath,
			"REF="+ref,
		)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("test script %s failed: %w", ts, err)
		}
	}
	return nil
}

// FinalizeFilesystems runs fstrim on the root and boot filesystems to improve
// compression ratios for sparse image files.
func (im *Image) FinalizeFilesystems(mountRootfs, mountBootfs, mountEfifs string) error {
	if mountRootfs == "" {
		return errors.New("missing mountRootfs parameter")
	}
	if mountBootfs == "" {
		return errors.New("missing mountBootfs parameter")
	}
	if mountEfifs == "" {
		return errors.New("missing mountEfifs parameter")
	}

	fmt.Fprintf(os.Stdout, "Executing fstrim on %s\n", mountRootfs)
	// fstrim may fail on USB sticks, so ignore errors.
	im.runner(nil, os.Stdout, os.Stderr, "fstrim", "-v", mountRootfs)

	fmt.Fprintf(os.Stdout, "Executing fstrim on %s\n", mountBootfs)
	im.runner(nil, os.Stdout, os.Stderr, "fstrim", "-v", mountBootfs)

	return nil
}

// Qcow2ImagePath returns the qcow2 image path for a given .img path.
func (im *Image) Qcow2ImagePath(imagePath string) (string, error) {
	if imagePath == "" {
		return "", errors.New("missing imagePath parameter")
	}
	return imagePath + ".qcow2", nil
}

// CreateQcow2Image creates a compressed qcow2 image from a raw image.
func (im *Image) CreateQcow2Image(imagePath string) error {
	if imagePath == "" {
		return errors.New("missing imagePath parameter")
	}
	qcow2Path, _ := im.Qcow2ImagePath(imagePath)
	return im.runner(nil, os.Stdout, os.Stderr,
		"qemu-img", "convert", "-c", "-O", "qcow2", "-p", imagePath, qcow2Path)
}

// ShowFinalFilesystemInfo displays information about the final filesystem layout.
func (im *Image) ShowFinalFilesystemInfo(blockDevice, mountBootfs, mountEfifs string) error {
	if blockDevice == "" {
		return errors.New("missing blockDevice parameter")
	}
	if mountBootfs == "" {
		return errors.New("missing mountBootfs parameter")
	}
	if mountEfifs == "" {
		return errors.New("missing mountEfifs parameter")
	}

	fmt.Fprintln(os.Stdout, "Final boot partition directory tree:")
	im.runner(nil, os.Stdout, os.Stderr, "find", mountBootfs)

	fmt.Fprintln(os.Stdout, "Final EFI partition directory tree:")
	im.runner(nil, os.Stdout, os.Stderr, "find", mountEfifs)

	fmt.Fprintf(os.Stdout, "Block devices on %s:\n", blockDevice)
	im.runner(nil, os.Stdout, os.Stderr, "blkid", blockDevice)

	fmt.Fprintln(os.Stdout, "Filesystem setup complete!")
	return nil
}

// ShowTestInfo prints information about generated artifacts and how to test them.
func (im *Image) ShowTestInfo(artifacts []string) {
	if len(artifacts) == 0 {
		fmt.Fprintln(os.Stderr, "show_test_info: missing artifacts array parameter")
		return
	}

	fmt.Fprintln(os.Stdout, "Generated artifacts:")
	for _, a := range artifacts {
		fmt.Fprintf(os.Stdout, ">> %s\n", a)
	}

	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "How to test:")
	fmt.Fprintln(os.Stdout, "$ vector dev vm -image IMAGE_PATH -memory 8G -interactive")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "To move to a USB stick:")
	fmt.Fprintln(os.Stdout, "    dd if=IMAGE_PATH of=/dev/sdX bs=4M conv=sparse,sync status=progress")
	fmt.Fprintln(os.Stdout)
}

// RemoveImageFile removes an image file and its associated .sha256 and .asc files.
func (im *Image) RemoveImageFile(imagePath string) error {
	if imagePath == "" {
		return errors.New("missing imagePath parameter")
	}

	fmt.Fprintf(os.Stdout, "Removing %s ...\n", imagePath)
	for _, path := range []string{imagePath, imagePath + ".sha256", imagePath + ".asc"} {
		os.Remove(path) // Ignore errors (file may not exist).
	}
	return nil
}

// ImageLockDir returns the image lock directory, creating it if necessary.
func (im *Image) ImageLockDir() (string, error) {
	lockDir, err := im.LockDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(lockDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create lock directory %s: %w", lockDir, err)
	}
	return lockDir, nil
}

// ImageLockPath returns the lock file path for a given ref.
func (im *Image) ImageLockPath(ref string) (string, error) {
	if ref == "" {
		return "", errors.New("missing ref parameter")
	}

	lockDir, err := im.ImageLockDir()
	if err != nil {
		return "", err
	}
	lockFile := filepath.Join(lockDir, ref+".lock")

	lockFileDir := filepath.Dir(lockFile)
	if err := os.MkdirAll(lockFileDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create lock file directory %s: %w", lockFileDir, err)
	}
	return lockFile, nil
}

// --- Utility functions ---

// copyFile copies src to dst, preserving content. It creates dst if it doesn't exist.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}
