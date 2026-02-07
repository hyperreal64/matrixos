#!/bin/bash
# preppers_lib.sh - shared library between all the preppers, to contains functions
#                   that are used OUTSIDE of chroots.
set -eu

source "${MATRIXOS_DEV_DIR:-/matrixos}"/headers/env.include.sh
source "${MATRIXOS_DEV_DIR}"/build/seeders/headers/preppersenv.include.sh

source "${MATRIXOS_DEV_DIR}"/lib/fs_lib.sh
source "${MATRIXOS_DEV_DIR}"/build/seeders/lib/seeders_lib.sh

preppers_lib.get_gpg_keychain_dir() {
    local kc_dir="${MATRIXOS_SEEDER_GPG_KEYS_DIR}"
    [[ ! -d "${kc_dir}" ]] && mkdir -p "${kc_dir}"
    echo "${kc_dir}"
}

preppers_lib.gpg_verify_file() {
    local filepath="${1}"
    local homedir
    homedir=$(preppers_lib.get_gpg_keychain_dir)
    echo "Verifying ${filepath} ..."
    gpg --homedir="${homedir}" --batch --yes --verify "${filepath}.asc" "${filepath}"
}

preppers_lib.gpg_verify_embedded_signature_file() {
    local filepath="${1}"
    local homedir
    homedir=$(preppers_lib.get_gpg_keychain_dir)
    echo "Verifying ${filepath} ..."
    gpg --homedir="${homedir}" --batch --yes --verify "${filepath}"
}

preppers_lib.gpg_decrypt_file() {
    local filepath="${1}"
    local outfilepath="${2}"
    local homedir
    homedir=$(preppers_lib.get_gpg_keychain_dir)
    echo "Decrypting ${filename} to ${outfilepath} ..."
    gpg --homedir="${homedir}" --batch --yes --decrypt --output "${outfilepath}" "${filepath}"
}

# Uses DOWNLOAD_DIR
preppers_lib.get_download_dir() {
    ddir="${DOWNLOAD_DIR}"
    [[ ! -d "${ddir}" ]] && mkdir -p "${ddir}"
    echo "${ddir}"
}

_is_rootfs_functional() {
    local chroot_dir="${1}"
    test -d "${chroot_dir}"
    test -x "${chroot_dir}/bin/sh"
    test -x "${chroot_dir}/bin/ls"
}

_move_chroot_dir_away() {
    local chroot_dir="${1}"
    echo
    local tmp_chroot_name
    tmp_chroot_name=$(basename "${chroot_dir}")
    local tmp_chroot_dir
    tmp_chroot_dir=$(mktemp -d --suffix="${tmp_chroot_name}" --tmpdir="$(dirname "${chroot_dir}")")
    echo "[${_seeder_name}] Executing mv ${chroot_dir} ${tmp_chroot_dir} ... in another 5 seconds ..."
    sleep 5
    mv "${chroot_dir}" "${tmp_chroot_dir}"
    mkdir -p "${chroot_dir}"
}

preppers_lib.sanity_check_chroot_dir() {
    local chroot_dir="${1}"
    local chroot_resume="${2}"
    local _seeder_name="${3}"

    if [ -z "${chroot_dir}" ]; then
        echo "Missing parameter to server prepper" >&2
        return 1
    fi
    if [ -e "${chroot_dir}" ] && [ ! -d "${chroot_dir}" ]; then
        echo "${chroot_dir} is not a directory ..." >&2
        return 1
    fi
    if [ ! -d "${chroot_dir}" ]; then
        echo "Creating chroot dir: ${chroot_dir} ..."
        # it is harmless if we are in the latter scenarios.
        mkdir -p "${chroot_dir}"
    fi

    fs_lib.check_dir_is_root "${chroot_dir}"
    fs_lib.check_active_mounts "${chroot_dir}"

    if [ -n "${chroot_resume}" ] && ! _is_rootfs_functional "${chroot_dir}"; then
        echo "[${_seeder_name}] Root filesystem at ${chroot_dir} is NOT functional ..." >&2
        echo "[${_seeder_name}] But you asked me to resume. You! So, what I am going to do is ..." >&2
        echo "[${_seeder_name}] Moving everything to a temp dir and starting over, in 10 seconds ..." >&2
        local c=
        for c in {1..10}; do
            echo -en "${c}."
            sleep 1
        done
        _move_chroot_dir_away "${chroot_dir}"

    elif [ -n "${chroot_resume}" ] && [ -d "${chroot_dir}" ]; then
        echo "[${_seeder_name}] Skipping stage3 unpacking ..."
        echo "[${_seeder_name}] Attempting to resume seeder in chroot: ${chroot_dir} ..."
        return 0
    elif [ -n "${chroot_resume}" ] && [ ! -d "${chroot_dir}" ]; then
        echo "[${_seeder_name}] Requested a chroot resume but chroot dir: ${chroot_dir} does not exist." >&2
        return 1
    elif [ -z "${chroot_resume}" ] && [ -d "${chroot_dir}" ] && [ -x "${chroot_dir}/bin/ls" ]; then
        echo "[${_seeder_name}] ${chroot_dir} exists and seems to be populated, while resume mode is not set." >&2
        echo "[${_seeder_name}] This seems a very suspicious situation, but I will continue nonetheless in 10 seconds." >&2
        echo "[${_seeder_name}] Press CTRL+C NOW if you think you made a mistake." >&2
        echo "[${_seeder_name}] Moving everything to a temp dir and starting over, in 10 seconds..."
        local c=
        for c in {1..10}; do
            echo -en "${c}."
            sleep 1
        done
        _move_chroot_dir_away "${chroot_dir}"
    fi

}

_get_prep_phase_path() {
    local chroot_dir="${1}"
    local prep_phase="${2}"
    echo "${chroot_dir%/}${MATRIXOS_PREPPERS_PHASES_STATE_DIR}/${prep_phase}.done"
}

preppers_lib.touch_done_prep_phase() {
    local chroot_dir="${1}"
    local prep_phase="${2}"
    if [ -z "${chroot_dir}" ]; then
        echo "preppers_lib.touch_done_prep_phase missing parameter." >&2
        return 1
    fi
    if [ -z "${prep_phase}" ]; then
        echo "preppers_lib.touch_done_prep_phase missing parameter." >&2
        return 1
    fi


    local prep_path
    prep_path="$(_get_prep_phase_path "${chroot_dir}" "${prep_phase}")"
    mkdir -p "$(dirname "${prep_path}")"
    touch "${prep_path}"
}

preppers_lib.is_prep_phase_done() {
    local chroot_dir="${1}"
    local prep_phase="${2}"
    if [ -z "${chroot_dir}" ]; then
        echo "preppers_lib.is_prep_phase_done missing parameter." >&2
        return 1
    fi
    if [ -z "${prep_phase}" ]; then
        echo "preppers_lib.is_prep_phase_done missing parameter." >&2
        return 1
    fi

    local prep_path
    prep_path="$(_get_prep_phase_path "${chroot_dir}" "${prep_phase}")"
    echo "Checking if prep is already done: ${prep_path}"
    test -f "${prep_path}"
}

preppers_lib.sanity_check_latest_bedrock() {
    local latest_bedrock="${1}"
    if [ -z "${latest_bedrock}" ]; then
        echo "Unable to find latest bedrock chroot dir." >&2
        return 1
    fi

    if [ ! -d "${latest_bedrock}" ]; then
        echo "Latest bedrock ${latest_bedrock} does not exist." >&2
        return 1
    fi
    # More sanity checks.
    local d=
    for d in /dev /proc sys; do
        if [ ! -d "${latest_bedrock}/${d}" ]; then
            echo "Latest bedrock ${latest_bedrock}/${d} does not exist." >&2
            return 1
        fi
    done
}

preppers_lib.create_build_metadata_file() {
    local chroot_dir="${1}"
    if [ -z "${chroot_dir}" ]; then
        echo "preppers_lib.create_build_metadata_file missing chroot_dir parameter." >&2
        return 1
    fi

    local bedrock_chroot_dir="${2}"
    if [ -z "${bedrock_chroot_dir}" ]; then
        echo "preppers_lib.create_build_metadata_file missing bedrock_chroot_dir parameter." >&2
        return 1
    fi

    local bedrock_name
    bedrock_name=$(basename "${bedrock_chroot_dir}")

    local build_metadata="${chroot_dir}/${MATRIXOS_SEEDER_BUILD_METADATA_FILE}"
    local build_metadata_dir
    build_metadata_dir=$(dirname "${build_metadata}")

    mkdir -p "${build_metadata_dir}"
    echo "Writing build metadata to ${build_metadata} ..."
    echo "BEDROCK_ORIGIN=${bedrock_name}" > "${build_metadata}"
    # SEEDER_CHROOT_NAME should be available, we just run in a subshell.
    if [ -n "${SEEDER_CHROOT_NAME}" ]; then
        echo "SEED_NAME=${SEEDER_CHROOT_NAME}" >> "${build_metadata}"
    else
        echo "WARNING: SEEDER_CHROOT_NAME not set!" >&2
    fi
    cat "${build_metadata}"
}

preppers_lib._rsync_copy() {
    local src="${1}"
    local dst="${2}"
    echo "Spawning rsync from ${src} to ${dst} ..."
    rsync \
        --archive \
        --verbose \
        --progress \
        --partial \
        -HAX \
        --numeric-ids \
        --delete-during \
        --one-file-system \
        "${src%/}/" "${dst%/}/"
}

preppers_lib._cp_reflink_copy() {
    local src="${1}"
    local dst="${2}"

    echo "Removing ${dst} ..."
    # It is safe to remove dst because parent func already checked if we have
    # active mounts.
    rm -rf "${dst}"

    echo "Spawning cp --preserve=links --reflink=auto from ${src} to ${dst} ..."
    cp -a --preserve=links --reflink=auto "${src}" "${dst}"
}

preppers_lib._rsync_from_bedrock() {
    local chroot_dir="${1}"
    local latest_bedrock="${2}"
    if [ -z "${latest_bedrock}" ]; then
        echo "preppers_lib.rsync_from_bedrock missing parameter." >&2
        return 1
    fi
    if [ -z "${chroot_dir}" ]; then
        echo "chroot_dir not set for prepper." >&2
        return 1
    fi

    # We need to create chroot_dir if we check for caps.
    mkdir -p "${chroot_dir}"

    # Active mounts check already done by sanity_check_chroot_dir.

    local use_cp="${MATRIXOS_PREPPERS_USE_CP_REFLINK_MODE_INSTEAD_OF_RSYNC}"

    if fs_lib.cp_reflink_copy_allowed "${latest_bedrock}" "${chroot_dir}" "${use_cp}"; then
        echo "Using experimental cp --reflink=auto copy mode ..."
        preppers_lib._cp_reflink_copy "${latest_bedrock}" "${chroot_dir}"
    else
        preppers_lib._rsync_copy "${latest_bedrock}" "${chroot_dir}"
    fi
    fs_lib.check_hardlink_preservation "${latest_bedrock}" "${chroot_dir}"

    preppers_lib.create_build_metadata_file "${chroot_dir}" "${latest_bedrock}"
}

preppers_lib.rsync_from_bedrock() {
    local chroot_dir="${1}"
    local latest_bedrock="${2}"
    if [ -z "${latest_bedrock}" ]; then
        echo "preppers_lib.rsync_from_bedrock missing parameter." >&2
        return 1
    fi
    if [ -z "${chroot_dir}" ]; then
        echo "chroot_dir not set for prepper." >&2
        return 1
    fi

    # Lock bedrock.
    local seeder_name
    seeder_name=$(seeders_lib.seeder_chroot_dir_to_name "${latest_bedrock}")
    seeders_lib.execute_with_seeder_lock "preppers_lib._rsync_from_bedrock" "${seeder_name}" \
        "${chroot_dir}" "${latest_bedrock}"
}

_prepper_dir="$(dirname "${0}")"
_seeders_dir="$(dirname "${_prepper_dir}")"

preppers_lib.find_latest_bedrock_chroot_dir() {
    (
        # Import the bedrock params in a subshell to avoid poisoning.
        source "${_seeders_dir}"/00-bedrock/params.sh
        bedrock_params.find_latest_chroot_dir "00-bedrock"
    )
}