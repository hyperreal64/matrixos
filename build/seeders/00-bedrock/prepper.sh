#!/bin/bash
# prepper.sh - Execute a prep script outside of the chroot, before the chroot is
#              ready to chroot into.
#
# These are the env variables that are made available to this prepper executable.
#
#
# CHROOT_DIR=/path/to/chroot
# The directory path to a chroot. Path can even be non-existent. Each prepper is
# tasked to decide what to do with it. The goal is to do whatever is necessary to
# prep CHROOT_DIR for chrooting and executing chroot.sh (the in-chroot seeder script).
#
#
# DOWNLOAD_DIR=
# The directory path in which files should be downloaded (e.g. stage3 files).
#
# CHROOT_RESUME=<*/1>
# If set to 1, it tells the prepper to prepare the chroot so that chroot.sh can resume
# the execution from the last checkpoint. If there's any difference at all, between resuming
# or not in your workflow.
#
# STAGE3_FILE=/path/to/stage3.tarball.xz.something
# If set, defines the stage3 tarball to use or has been used (if the prepper is not the
# first one).
#
# STAGE3_URL=https://path/to/a/stage3/download/url/of/sorts
# If set, defines the Gentoo stage3 URL that must be used to download a stage3 file.
# It can be also a URL pointing to a file name (in its content) that should be downloaded
# afterwards.
set -eu

_prepper_dir="$(dirname "${0}")"
_seeders_dir="$(dirname "${_prepper_dir}")"
_seeder_name="$(basename "${_prepper_dir}")"

source "${MATRIXOS_DEV_DIR}"/headers/env.include.sh
source "${MATRIXOS_DEV_DIR}"/lib/fs_lib.sh
source "${MATRIXOS_DEV_DIR}"/build/seeders/lib/preppers_lib.sh


download_latest_stage3() {
    local url="${1}"
    local filename=
    filename=$(basename "${url}")
    local download_dir=
    download_dir="$(preppers_lib.get_download_dir)"
    local download_path="${download_dir}/${filename}"

    echo "Downloading from ${url} ..."
    wget --quiet "${url}" -O "${download_path}"
    preppers_lib.gpg_verify_embedded_signature_file "${download_path}"

    # If filename ends with .txt it's probably containing the real file name to
    # download.
    if [[ "${filename}" == *.txt ]]; then
        echo "The filename ends with .txt, reading contents ..."
        local contents_path="${download_path}.contents"
        preppers_lib.gpg_decrypt_file "${download_path}" "${contents_path}"
        local stage3_rel_path=
        stage3_rel_path=$(awk '/^[^#]/ {print $1; exit}' "${contents_path}")
        if [ -z "${stage3_rel_path}" ]; then
            echo "Unable to find stage3 filename in ${contents_path}" >&2
            return 1
        fi

        local base_url=
        base_url=$(dirname "${url}")
        url="${base_url}/${stage3_rel_path}"
        filename=$(basename "${url}")
        download_path="${download_dir}/${filename}"

        local tmp_file=
        tmp_file=$(fs_lib.create_temp_file "${download_dir}" "${filename}")
        local tmp_asc_file=
        tmp_asc_file=$(fs_lib.create_temp_file "${download_dir}" "${filename}.asc")

        if [ ! -f "${download_path}" ] || [ ! -f "${download_path}.asc" ]; then
            echo "Downloading real stage3 from ${url} ..."

            wget --quiet "${url}" -O "${tmp_file}"
            mv "${tmp_file}" "${download_path}"

            wget --quiet "${url}.asc" -O "${tmp_asc_file}"
            mv "${tmp_asc_file}" "${download_path}.asc"
        else
            echo "${download_path}* already existing." >&2
        fi
    fi

    # Communicate through side effects to the caller.
    STAGE3_FILE="${download_path}"
}

unpack_stage3_file() {
    local stage3_file="${1}"
    local unpack_dir="${2}"
    echo "Unpacking stage3 file ${stage3_file} to ${unpack_dir} ..."
    tar -xf "${stage3_file}" -C "${unpack_dir}"
}

bedrock_prepper.prepare_rootfs() {
    if [ -z "${STAGE3_FILE}" ]; then
        download_latest_stage3 "${STAGE3_URL}"
    fi
    if [ -z "${STAGE3_FILE}" ]; then
        echo "[${_seeder_name}] Unable to determine the stage3 file to work with." >&2
        return 1
    fi

    echo "[${_seeder_name}] Verifying stage3 file ${STAGE3_FILE} ..."
    preppers_lib.gpg_verify_file "${STAGE3_FILE}"
    unpack_stage3_file "${STAGE3_FILE}" "${CHROOT_DIR}"
}

bedrock_prepper.create_build_metadata() {
    preppers_lib.create_build_metadata_file "${CHROOT_DIR}" "${CHROOT_DIR}"
}

main() {
    preppers_lib.sanity_check_chroot_dir "${CHROOT_DIR}" "${CHROOT_RESUME}" "${_seeder_name}"

    local prep_phases=(
        bedrock_prepper.prepare_rootfs
        bedrock_prepper.create_build_metadata
    )
    local phase_f=
    for phase_f in "${prep_phases[@]}"; do
        local phase_name="${_seeder_name}/${phase_f}"

        if ! preppers_lib.is_prep_phase_done "${CHROOT_DIR}" "${phase_name}"; then
            echo "[${_seeder_name}] Executing prep phase: ${phase_f} ..."
            "${phase_f}"
            preppers_lib.touch_done_prep_phase "${CHROOT_DIR}" "${phase_name}"
        else
            echo "[${_seeder_name}] ${phase_f} already finished."
        fi
    done
}

main "${@}"
