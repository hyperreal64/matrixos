package filesystems

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"matrixos/vector/lib/config"
	"matrixos/vector/lib/runner"
)

// IFsenc defines the interface for filesystem encryption operations.
// It mirrors all public methods of Fsenc for testability.
type IFsenc interface {
	// Config accessors
	EncryptionEnabled() (bool, error)
	EncryptionKey() (string, error)
	EncryptedRootFsName() (string, error)
	OsName() (string, error)

	// Operations
	LuksEncrypt(devicePath, desiredLuksDevice string, deviceMappers *[]string) error
	LuksBackupHeader(devicePath, mountEfifs string) error
	ValidateLuksVariables() error
}

// Fsenc provides filesystem encryption operations backed by LUKS/cryptsetup.
type Fsenc struct {
	cfg    config.IConfig
	runner runner.Func
}

// NewFsenc creates a new Fsenc instance.
func NewFsenc(cfg config.IConfig) (*Fsenc, error) {
	if cfg == nil {
		return nil, errors.New("missing config parameter")
	}
	return &Fsenc{
		cfg:    cfg,
		runner: runner.Run,
	}, nil
}

// EncryptionEnabled returns whether rootfs encryption is enabled.
func (f *Fsenc) EncryptionEnabled() (bool, error) {
	return f.cfg.GetBool("Imager.Encryption")
}

// EncryptionKey returns the configured encryption key (passphrase or file path).
func (f *Fsenc) EncryptionKey() (string, error) {
	key, err := f.cfg.GetItem("Imager.EncryptionKey")
	if err != nil {
		return "", err
	}
	return key, nil
}

// EncryptedRootFsName returns the configured LUKS device-mapper name for the
// encrypted root filesystem.
func (f *Fsenc) EncryptedRootFsName() (string, error) {
	name, err := f.cfg.GetItem("Imager.EncryptedRootFsName")
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", errors.New("invalid Imager.EncryptedRootFsName")
	}
	return name, nil
}

// OsName returns the OS name as defined in the config.
func (f *Fsenc) OsName() (string, error) {
	name, err := f.cfg.GetItem("matrixOS.OsName")
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", errors.New("invalid matrixOS.OsName")
	}
	return name, nil
}

// LuksEncrypt formats a device with LUKS encryption and opens it.
//
// devicePath is the block device to encrypt (e.g. a partition on a loop device).
// desiredLuksDevice is the full /dev/mapper/<name> path expected after opening.
// deviceMappers is a pointer to the caller's slice that tracks opened device-mapper
// names for cleanup; the LUKS name is appended on success.
func (f *Fsenc) LuksEncrypt(devicePath, desiredLuksDevice string, deviceMappers *[]string) error {
	if devicePath == "" {
		return errors.New("missing devicePath parameter")
	}
	if desiredLuksDevice == "" {
		return errors.New("missing desiredLuksDevice parameter")
	}
	if deviceMappers == nil {
		return errors.New("missing deviceMappers parameter")
	}

	encKey, err := f.EncryptionKey()
	if err != nil {
		return fmt.Errorf("failed to retrieve encryption key: %w", err)
	}

	var stdin io.Reader
	var keyFileArg string

	if FileExists(encKey) {
		fmt.Fprintln(os.Stdout, "LUKS Encryption key is a file.")
		keyFileArg = encKey
		stdin = nil
	} else {
		fmt.Fprintln(os.Stdout, "LUKS Encryption key is NOT a file.")
		keyFileArg = "-"
		stdin = strings.NewReader(encKey)
	}

	luksName := filepath.Base(desiredLuksDevice)

	// Format the device with LUKS encryption.
	fmt.Fprintf(os.Stdout, "Formatting %s using LUKS Encryption ...\n", devicePath)
	err = f.runner(
		stdin, os.Stdout, os.Stderr,
		"cryptsetup",
		"-c", "aes-xts-plain64",
		"-s", "512",
		"luksFormat",
		devicePath,
		keyFileArg,
	)
	if err != nil {
		return fmt.Errorf("cryptsetup luksFormat failed on %s: %w", devicePath, err)
	}

	// Re-create stdin for the open command if the key is not a file.
	if !FileExists(encKey) {
		stdin = strings.NewReader(encKey)
	} else {
		stdin = nil
	}

	// Open the LUKS device.
	fmt.Fprintf(os.Stdout, "Opening LUKS device %s ...\n", devicePath)
	err = f.runner(
		stdin, os.Stdout, os.Stderr,
		"cryptsetup",
		"open",
		"--allow-discards",
		"--key-file="+keyFileArg,
		devicePath,
		luksName,
	)
	if err != nil {
		return fmt.Errorf("cryptsetup open failed on %s: %w", devicePath, err)
	}

	// Track the opened device-mapper name for cleanup.
	*deviceMappers = append(*deviceMappers, luksName)

	// Wait for the device node to appear.
	DevicesSettle()

	if _, err := os.Stat(desiredLuksDevice); err != nil {
		return fmt.Errorf("%s does not exist: cannot set up LUKS Encryption: %w", desiredLuksDevice, err)
	}

	return nil
}

// LuksBackupHeader creates a backup of the LUKS header on the EFI partition.
//
// devicePath is the LUKS-encrypted block device.
// mountEfifs is the mount point of the EFI filesystem where the backup will be stored.
func (f *Fsenc) LuksBackupHeader(devicePath, mountEfifs string) error {
	if devicePath == "" {
		return errors.New("missing devicePath parameter")
	}
	if mountEfifs == "" {
		return errors.New("missing mountEfifs parameter")
	}

	osName, err := f.OsName()
	if err != nil {
		return fmt.Errorf("failed to retrieve OS name: %w", err)
	}

	backupFile := filepath.Join(mountEfifs, osName+"-rootfs-luks-header-backup.img")

	fmt.Fprintln(os.Stdout, "Creating a backup of the LUKS encryption headers on the EFI partition ...")
	err = f.runner(
		nil, os.Stdout, os.Stderr,
		"cryptsetup",
		"luksHeaderBackup",
		devicePath,
		"--header-backup-file",
		backupFile,
	)
	if err != nil {
		return fmt.Errorf("cryptsetup luksHeaderBackup failed for %s: %w", devicePath, err)
	}

	return nil
}

// ValidateLuksVariables checks that all required LUKS-related configuration
// variables are set when encryption is enabled. This mirrors
// imager_env.validate_luks_variables() from imagerenv.include.sh.
func (f *Fsenc) ValidateLuksVariables() error {
	enabled, err := f.EncryptionEnabled()
	if err != nil {
		return fmt.Errorf("failed to check Imager.Encryption: %w", err)
	}

	if !enabled {
		return nil
	}

	fmt.Fprintln(os.Stdout, "Encryption of rootfs enabled. Setting up...")

	key, err := f.EncryptionKey()
	if err != nil {
		return fmt.Errorf("failed to retrieve Imager.EncryptionKey: %w", err)
	}
	if key == "" {
		return errors.New("Imager.EncryptionKey not set: please set it to a passphrase")
	}

	rootfsName, err := f.EncryptedRootFsName()
	if err != nil {
		return fmt.Errorf("failed to retrieve Imager.EncryptedRootFsName: %w", err)
	}
	if rootfsName == "" {
		return errors.New("Imager.EncryptedRootFsName is unset: please set it to a devmapper name")
	}

	return nil
}
