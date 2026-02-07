#!/bin/bash
# This file is sourced inside release scripts.
# It contains common releaser execution variables.
set -eu

if [ -z "${__MATRIXOS_IMAGER_ENV_PARSED:-}" ]; then

source "${MATRIXOS_DEV_DIR}/lib/env_lib.sh"

MATRIXOS_LIVEOS_IMAGE_SIZE=$(env_lib.get_simple_var "Imager" "ImageSize")
MATRIXOS_LIVEOS_EFI_SIZE=$(env_lib.get_simple_var "Imager" "EfiPartitionSize")
MATRIXOS_LIVEOS_BOOT_SIZE=$(env_lib.get_simple_var "Imager" "BootPartitionSize")
MATRIXOS_LIVEOS_IMAGES_COMPRESSOR="$(env_lib.get_simple_var "Imager" "Compressor")"
MATRIXOS_IMAGES_OUT_DIR=$(env_lib.get_root_var "${MATRIXOS_DEV_DIR}" "Imager" "ImagesDir")
MATRIXOS_IMAGES_MOUNT_DIR=$(env_lib.get_root_var "${MATRIXOS_DEV_DIR}" "Imager" "MountDir")
MATRIXOS_LIVEOS_ENCRYPTION=$(env_lib.get_bool_var "Imager" "Encryption")
MATRIXOS_LIVEOS_ENCRYPTION_KEY=$(env_lib.get_simple_var "Imager" "EncryptionKey")
MATRIXOS_LIVEOS_ENCRYPTION_ROOTFS_NAME=$(env_lib.get_simple_var "Imager" "EncryptedRootFsName")
MATRIXOS_LIVEOS_ESP_PARTTYPE=$(env_lib.get_simple_var "Imager" "EspPartitionType")
MATRIXOS_LIVEOS_BOOT_PARTTYPE=$(env_lib.get_simple_var "Imager" "BootPartitionType")
MATRIXOS_LIVEOS_ROOT_PARTTYPE=$(env_lib.get_simple_var "Imager" "RootPartitionType")

# MATRIXOS_IMAGE_LOCK_DIR=/path/to/locks/dir
# Directory used by imager to contain file locks for coordinating image management.
MATRIXOS_IMAGE_LOCK_DIR=$(env_lib.get_root_var "${MATRIXOS_DEV_DIR}" "Imager" "LocksDir")

# MATRIXOS_IMAGE_LOCK_WAIT_SECS=secs
# Number of seconds to wait before giving up on waiting for an imager file lock.
MATRIXOS_IMAGE_LOCK_WAIT_SECS=$(env_lib.get_simple_var "Imager" "LockWaitSeconds")


imager_env.validate_luks_variables() {
    if [ -n "${MATRIXOS_LIVEOS_ENCRYPTION}" ]; then
        echo "Encryption of rootfs enabled. Setting up..."
        if [ -z "${MATRIXOS_LIVEOS_ENCRYPTION_KEY}" ]; then
            echo "MATRIXOS_LIVEOS_ENCRYPTION_KEY not set. Please set it to a passprhase." >&2
            return 1
        fi
        if [ -z "${MATRIXOS_LIVEOS_ENCRYPTION_ROOTFS_NAME}" ]; then
            echo "MATRIXOS_LIVEOS_ENCRYPTION_ROOTFS_NAME is unset. Please set it to a devmapper name." >&2
            return 1
        fi
    fi
}

__MATRIXOS_IMAGER_ENV_PARSED=1
fi