#!/bin/bash
# This file is sourced inside seeders chroot.sh (inside chroot) scripts.
# It contains common seeder execution variables.
set -eu

source "${MATRIXOS_DEV_DIR:-/matrixos}"/headers/env.include.sh


# MATRIXOS_SEEDERS_BUILD_ARTIFACTS_DIR=/build
# Directory in which seeders are allowed to store their own artifacts.
# This directory will be removed during release.
MATRIXOS_SEEDERS_BUILD_ARTIFACTS_DIR="/build"

# MATRIXOS_SEEDERS_DIR=/matrixos/build/seeders
# Directory in which (currently) seeders scripts for execution inside chroot
# are stored.
# This MUST always use the default DEFAULT_MATRIXOS_DEV_DIR path.
MATRIXOS_SEEDERS_DIR="${DEFAULT_MATRIXOS_DEV_DIR}/build/seeders"

# MATRIXOS_SEEDERS_PHASES_STATE_DIR=/build/.seeders_phases
# Directory in which seeders can store their checkpoints (or phases state)
# To allow for checkpointed resume of execution.
MATRIXOS_SEEDERS_PHASES_STATE_DIR="${MATRIXOS_SEEDERS_BUILD_ARTIFACTS_DIR}/.seeders_phases"

# MATRIXOS_SEEDER_DONE_FLAG_FILE=${MATRIXOS_SEEDERS_PHASES_STATE_DIR}/seeder.complete_{{.seeder_name}}
# File that is atomically created by the seeder orchestrator to flag that a
# chroot seeder is fully done and all ephemeral mount points are removed.
# Note that the file name gets the name of the seeder appended at the end.
MATRIXOS_SEEDER_DONE_FLAG_FILE="${MATRIXOS_SEEDERS_PHASES_STATE_DIR}/seeder.complete"

# MATRIXOS_DISABLED_SEEDER_FILE=name
# File that can be placed inside the seeder directory (e.g. 10-server) to disable the
# Seeder from being executed.
MATRIXOS_DISABLED_SEEDER_FILE="__disabled__"

# MATRIXOS_SEEDER_CHROOT_EXEC_NAME=name.sh
# Name of the script used inside the chroot to seed the filesystem (e.g. install ebuilds).
MATRIXOS_SEEDER_CHROOT_EXEC_NAME="chroot.sh"

# MATRIXOS_SEEDER_PARAMS_EXEC_NAME=name.sh
# Name of the script containing important environment variables for the associated seeder, that
# are used by the seeding process.
MATRIXOS_SEEDER_PARAMS_EXEC_NAME="params.sh"

# MATRIXOS_SEEDER_CHROOT_DATE=
# Overrides the default dating scheme used below "YYYYMMDD" anchored to the
# first past monday.
seeders_env.get_chroot_date() {
    if [ -n "${MATRIXOS_SEEDER_CHROOT_DATE:-}" ]; then
        echo "Overridden MATRIXOS_SEEDER_CHROOT_DATE: ${MATRIXOS_SEEDER_CHROOT_DATE}" >&2
        echo "${MATRIXOS_SEEDER_CHROOT_DATE}"
        return 0
    fi
    date -d "$(( $(date +%u) - 1 )) days ago" +%Y%m%d
}

seeders_env.get_chroot_seeder_done_flag_file() {
    local seeder_name="${1}"
    if [ -z "${seeder_name}" ]; then
        echo "${0}: missing seeder name parameter" >&2
        return 1
    fi
    local chroot_dir="${2}"
    if [ -z "${chroot_dir}" ]; then
        echo "${0}: missing chroot dir parameter" >&2
        return 1
    fi

    local flag_path="${chroot_dir%/}${MATRIXOS_SEEDER_DONE_FLAG_FILE}_${seeder_name}"
    echo "${flag_path}"
}