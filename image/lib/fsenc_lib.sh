#!/bin/bash
# matrixOS filesystem encryption setup library.
set -e

source "${MATRIXOS_DEV_DIR:-/matrixos}/headers/env.include.sh"
source "${MATRIXOS_DEV_DIR}"/image/headers/imagerenv.include.sh


fsenc_lib.mount_image_as_loop_device() {
    local image_path="${1}"
    if [ -z "${image_path}" ]; then
        echo "fsenc_lib.mount_image_as_loop_device: missing image_path parameter" >&2
        return 1
    fi

    local loop_device=
    loop_device=$(losetup --show -fP "${image_path}")
    if [ -z "${loop_device}" ]; then
        echo "Unable to set up loop device for ${image_path}" >&2
        return 1
    fi
    echo "${loop_device}"
}

fsenc_lib.luks_encrypt() {
    # DO NOT CALL THIS IN A SUBSHELL OR THE CALLER ARRAY CHANGE WILL BE LOST.

    local device_path="${1}"
    if [ -z "${device_path}" ]; then
        echo "fsenc_lib.luks_encrypt: missing device_path parameter" >&2
        return 1
    fi

    local desired_luks_device="${2}"
    if [ -z "${desired_luks_device}" ]; then
        echo "fsenc_lib.luks_encrypt: missing desired_luks_device parameter" >&2
        return 1
    fi

    local -n device_mappers="${3}"
    if [ -z "${3}" ]; then
        echo "fsenc_lib.luks_encrypt: missing device_mappers parameter" >&2
        return 1
    fi

    local enc_key_stdin=
    local enc_key_file=
    if [ -f "${MATRIXOS_LIVEOS_ENCRYPTION_KEY}" ]; then
        echo "LUKS Encryption key is a file."
        enc_key_file="${MATRIXOS_LIVEOS_ENCRYPTION_KEY}"
    else
        echo "LUKS Encryption key is NOT a file."
        enc_key_file="-"
        enc_key_stdin="${MATRIXOS_LIVEOS_ENCRYPTION_KEY}"
    fi
    local luks_name=
    luks_name=$(basename "${desired_luks_device}")

    echo "Formatting ${device_path} using LUKS Encryption ..."
    printf "${enc_key_stdin}" | cryptsetup -c aes-xts-plain64 -s 512 luksFormat \
        "${device_path}" "${enc_key_file}"

    echo "Opening LUKS device ${device_path} ..."
    printf "${enc_key_stdin}" | cryptsetup open --allow-discards --key-file="${enc_key_file}" \
        "${device_path}" "${luks_name}"
    # Update caller tracking variables of resources so that they can be disposed on exit.
    device_mappers+=( "${luks_name}" )

    # Wait for the loop device to be really ready.
    udevadm settle

    if [ ! -e "${desired_luks_device}" ]; then
        echo "${desired_luks_device} does not exist. Cannot set up LUKS Encryption." >&2
        return 1
    fi
}

fsenc_lib.luks_backup_header() {
    local device_path="${1}"
    if [ -z "${device_path}" ]; then
        echo "fsenc_lib.luks_backup_header: missing device_path parameter" >&2
        return 1
    fi

    local mount_efifs="${2}"
    if [ -z "${mount_efifs}" ]; then
        echo "fsenc_lib.luks_backup_header: missing mount_efifs parameter" >&2
        return 1
    fi

    echo "Creating a backup of the LUKS encryption headers on the EFI partition ..."
    cryptsetup luksHeaderBackup \
        "${device_path}" \
        --header-backup-file \
        "${mount_efifs}/${MATRIXOS_OSNAME}-rootfs-luks-header-backup.img"
}