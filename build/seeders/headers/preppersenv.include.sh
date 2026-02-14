#!/bin/bash
# This file is sourced inside preppers prepper.sh (outside chroot) scripts.
# It contains common prepper execution variables.
set -eu

if [ -z "${__MATRIXOS_PREPPERS_ENV_PARSED:-}" ]; then

source "${MATRIXOS_DEV_DIR}"/lib/env_lib.sh

# See conf/matrixos.conf for documentation on these variables.
MATRIXOS_SEEDER_METADATA_DIR=$(env_lib.get_simple_var "Seeder" "ChrootMetadataDir")
MATRIXOS_SEEDER_BUILD_METADATA_FILE="${MATRIXOS_SEEDER_METADATA_DIR}/$(env_lib.get_simple_var "Seeder" "ChrootMetadataDirBuildFileName")"
MATRIXOS_PREPPERS_BUILD_ARTIFACTS_DIR=$(env_lib.get_simple_var "Seeder" "ChrootBuildArtifactsDir")
MATRIXOS_PREPPERS_PHASES_STATE_DIR=$(env_lib.get_simple_var "Seeder" "ChrootPreppersPhasesStateDir")
MATRIXOS_PREPPERS_USE_CP_REFLINK_MODE_INSTEAD_OF_RSYNC=$(env_lib.get_bool_var "Seeder" "UseCpReflinkModeInsteadOfRsync")
MATRIXOS_SEEDER_PREPPER_EXEC_NAME=$(env_lib.get_simple_var "Seeder" "PrepperExecutableName")
MATRIXOS_SEEDER_LOCK_DIR=$(env_lib.get_root_var "${MATRIXOS_DEV_DIR}" "Seeder" "LocksDir")
MATRIXOS_SEEDER_LOCK_WAIT_SECS=$(env_lib.get_simple_var "Seeder" "LockWaitSeconds")

__MATRIXOS_PREPPERS_ENV_PARSED=1
fi