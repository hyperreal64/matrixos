#!/bin/bash
# This file is sourced inside seeders chroot.sh (inside chroot) scripts.
# It contains common seeder execution variables.
set -eu

if [ -z "${__MATRIXOS_SEEDERS_ENV_PARSED:-}" ]; then

source "${MATRIXOS_DEV_DIR}"/lib/env_lib.sh

# See conf/matrixos.conf for documentation on these variables.
MATRIXOS_SEEDERS_DIR=$(env_lib.get_simple_var "matrixOS" "DefaultRoot")/build/seeders
MATRIXOS_SEEDERS_BUILD_ARTIFACTS_DIR=$(env_lib.get_simple_var "Seeder" "ChrootBuildArtifactsDir")
MATRIXOS_SEEDERS_PHASES_STATE_DIR=$(env_lib.get_simple_var "Seeder" "ChrootSeedersPhasesStateDir")
MATRIXOS_DISABLED_SEEDER_FILE=$(env_lib.get_simple_var "Seeder" "SeederDisabledFileName")
MATRIXOS_USE_LOCAL_GIT_REPO_INSIDE_CHROOT=$(env_lib.get_bool_var "Seeder" "UseLocalGitRepoInsideChroot")
MATRIXOS_DELETE_DOT_GIT_FROM_GIT_REPO=$(env_lib.get_bool_var "Seeder" "DeleteDotGitFromGitRepo")

_seeder_flag_prefix=$(env_lib.get_simple_var "Seeder" "ChrootSeederDoneFlagFileNamePrefix")
MATRIXOS_SEEDER_DONE_FLAG_FILE="${MATRIXOS_SEEDERS_PHASES_STATE_DIR}/${_seeder_flag_prefix}"

MATRIXOS_SEEDER_CHROOT_EXEC_NAME=$(env_lib.get_simple_var "Seeder" "ChrootExecutableName")
MATRIXOS_SEEDER_PARAMS_EXEC_NAME=$(env_lib.get_simple_var "Seeder" "ParamsExecutableName")

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

__MATRIXOS_SEEDERS_ENV_PARSED=1
fi