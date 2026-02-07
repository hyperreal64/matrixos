#!/bin/bash
# This file is sourced inside release scripts.
# It contains common releaser execution variables.
set -eu

if [ -z "${__MATRIXOS_RELEASER_ENV_PARSED:-}" ]; then

source "${MATRIXOS_DEV_DIR}"/lib/env_lib.sh

# See conf/matrixos.conf for documentation on these variables.
MATRIXOS_RELEASE_HOSTNAME=$(env_lib.get_simple_var "Releaser" "Hostname")
MATRIXOS_RELEASE_HOOKS_DIR=$(env_lib.get_root_var "${MATRIXOS_DEV_DIR}" "Releaser" "HooksDir")
MATRIXOS_RELEASER_USE_CP_REFLINK_MODE_INSTEAD_OF_RSYNC=$(env_lib.get_bool_var "Releaser" "UseCpReflinkModeInsteadOfRsync")
MATRIXOS_RELEASE_LOCK_DIR=$(env_lib.get_root_var "${MATRIXOS_DEV_DIR}" "Releaser" "LocksDir")
MATRIXOS_RELEASE_LOCK_WAIT_SECS=$(env_lib.get_simple_var "Releaser" "LockWaitSeconds")

__MATRIXOS_RELEASER_ENV_PARSED=1
fi