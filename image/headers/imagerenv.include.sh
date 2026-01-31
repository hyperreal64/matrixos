#!/bin/bash
# This file is sourced inside release scripts.
# It contains common releaser execution variables.
set -e

source "${MATRIXOS_DEV_DIR:-/matrixos}/headers/env.include.sh"

# Define the sizes of the matrixOS partitions and whole image minimum disk size requirements.
MATRIXOS_LIVEOS_IMAGE_SIZE="${MATRIXOS_LIVEOS_IMAGE_SIZE:-32G}"
MATRIXOS_LIVEOS_EFI_SIZE="${MATRIXOS_LIVEOS_EFI_SIZE:-200M}"
MATRIXOS_LIVEOS_BOOT_SIZE="${MATRIXOS_LIVEOS_BOOT_SIZE:-1G}"

# Use qemu-img to create an additional qcow2 image for the generated .img files.
# Set to 1 to make this the default.
MATRIXOS_LIVEOS_CREATE_QCOW2="${MATRIXOS_LIVEOS_CREATE_QCOW2:-}"

# Define the compressor to use to compress the .img files. Works great for unencrypted images.
# Should work ok too for encrypted images that correctly implemented sparse file support.
MATRIXOS_LIVEOS_IMAGES_COMPRESSOR="${MATRIXOS_LIVEOS_IMAGES_COMPRESSOR:-xz -f -0 -T0}"

# Define the directory where images are written to by imager.
MATRIXOS_IMAGES_OUT_DIR="${MATRIXOS_IMAGES_OUT_DIR:-${MATRIXOS_OUT_DIR}/images}"
# Directory where temporary mounts are placed during the image generation phase.
MATRIXOS_IMAGES_MOUNT_DIR="${MATRIXOS_IMAGES_MOUNT_DIR:-${MATRIXOS_OUT_DIR}/mounts}"

# Encrypted root filesystem setup.
MATRIXOS_LIVEOS_ENCRYPTION="${MATRIXOS_LIVEOS_ENCRYPTION:-}"
MATRIXOS_LIVEOS_ENCRYPTION_KEY="${MATRIXOS_LIVEOS_ENCRYPTION_KEY:-MatrixOS2026Enc}"
MATRIXOS_LIVEOS_ENCRYPTION_ROOTFS_NAME="${MATRIXOS_LIVEOS_ENCRYPTION_ROOTFS_NAME:-${MATRIXOS_OSNAME}root}"

MATRIXOS_LIVEOS_ESP_PARTTYPE="${MATRIXOS_LIVEOS_ESP_PARTTYPE:-C12A7328-F81F-11D2-BA4B-00A0C93EC93B}"  # ESP
MATRIXOS_LIVEOS_BOOT_PARTTYPE="${MATRIXOS_LIVEOS_BOOT_PARTTYPE:-BC13C2FF-59E6-4262-A352-B275FD6F7172}" # XBOOTLDR
MATRIXOS_LIVEOS_ROOT_PARTTYPE="${MATRIXOS_LIVEOS_ROOT_PARTTYPE:-4F68BCE3-E8CD-4DB1-96E7-FBCAF984B709}"  # x86-64

# MATRIXOS_IMAGE_LOCK_DIR=/path/to/locks/dir
# Directory used by imager to contain file locks for coordinating image management.
MATRIXOS_IMAGE_LOCK_DIR="${MATRIXOS_IMAGE_LOCK_DIR:-"${MATRIXOS_LOCKS_DIR}/image"}"

# MATRIXOS_IMAGE_LOCK_WAIT_SECS=secs
# Number of seconds to wait before giving up on waiting for an imager file lock.
MATRIXOS_IMAGE_LOCK_WAIT_SECS=${MATRIXOS_IMAGE_LOCK_WAIT_SECS:-86400}


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