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

# Import the preppers_lib functions.
source "${_seeders_dir}"/lib/preppers_lib.sh

gnome_prepper.sync_from_bedrock() {
    local latest_bedrock=
    latest_bedrock=$(preppers_lib.find_latest_bedrock_chroot_dir)
    preppers_lib.sanity_check_latest_bedrock "${latest_bedrock}"

    echo "[${_seeder_name}] Starting prepper in ${CHROOT_DIR} using Bedrock: ${latest_bedrock} ..."
    preppers_lib.rsync_from_bedrock "${CHROOT_DIR}" "${latest_bedrock}"
}

main() {
    echo "[${_seeder_name}] Starting prepper ..."
    preppers_lib.sanity_check_chroot_dir "${CHROOT_DIR}" "${CHROOT_RESUME}" "${_seeder_name}"
    if [ ! -d "${CHROOT_DIR}" ]; then
        echo "[${_seeder_name}] Creating chroot dir: ${CHROOT_DIR} ..."
        mkdir -p "${CHROOT_DIR}"
    fi

    local prep_phases=(
        gnome_prepper.sync_from_bedrock
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