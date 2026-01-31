#!/bin/bash
# This file is sourced inside preppers prepper.sh (outside chroot) scripts.
# It contains common prepper execution variables.
set -eu

# MATRIXOS_SEEDER_METADATA_DIR=/some/directory/path/inside/chroot
# This directory contains text files with some release/origin metadata useful for release
# versioning information generation or debugging.
# It is purposely placed in /usr to keep it read-only.
MATRIXOS_SEEDER_METADATA_DIR="${MATRIXOS_SEEDER_METADATA_DIR:-/usr/share/matrixos/seeder/metadata}"
# MATRIXOS_SEEDER_BUILD_METADATA_FILE=/path/to/some/text/file/inside/chroot
# This file contains machine readable (env vars style) build information.
MATRIXOS_SEEDER_BUILD_METADATA_FILE="${MATRIXOS_SEEDER_BUILD_METADATA_FILE:-${MATRIXOS_SEEDER_METADATA_DIR}/build}"

# MATRIXOS_PREPPERS_BUILD_ARTIFACTS_DIR=/build
# Directory in which preppers are allowed to store their own artifacts.
# This directory will be removed during release. This path is relative to
# a chroot directory.
MATRIXOS_PREPPERS_BUILD_ARTIFACTS_DIR=/build

# MATRIXOS_PREPPERS_PHASES_STATE_DIR=/build/seeders/.seeders_phases
# Directory in which seeders can store their checkpoints (or phases state)
# To allow for checkpointed resume of execution. This path is relative to
# a chroot directory.
MATRIXOS_PREPPERS_PHASES_STATE_DIR="${MATRIXOS_PREPPERS_BUILD_ARTIFACTS_DIR}/preppers/.preppers_phases"

# MATRIXOS_PREPPERS_USE_CP_REFLINK_MODE_INSTEAD_OF_RSYNC=1
# To enable preppers to clone parent chroots using cp --reflink=auto instead of using
# rsync. This effectively reduces disk space by a lot when maintaining several builds for
# filesystems that support it.
MATRIXOS_PREPPERS_USE_CP_REFLINK_MODE_INSTEAD_OF_RSYNC=${MATRIXOS_PREPPERS_USE_CP_REFLINK_MODE_INSTEAD_OF_RSYNC:-1}

# MATRIXOS_SEEDER_PREPPER_EXEC_NAME=name.sh
# Name of the script used to prepare the seed chroot before entering it. Can be used to derive a root
# fs from another seed, or prepare the environment before entering the new root.
MATRIXOS_SEEDER_PREPPER_EXEC_NAME="prepper.sh"

# MATRIXOS_SEEDER_LOCK_DIR=/path/to/locks/dir
# Directory used by preppers to contain file locks for coordinating seeders chroots management.
MATRIXOS_SEEDER_LOCK_DIR="${MATRIXOS_SEEDER_LOCK_DIR:-"${MATRIXOS_LOCKS_DIR}/seeders"}"

# MATRIXOS_SEEDER_LOCK_WAIT_SECS=secs
# Number of seconds to wait before giving up on waiting for a seeder chroot file lock.
MATRIXOS_SEEDER_LOCK_WAIT_SECS=${MATRIXOS_SEEDER_LOCK_WAIT_SECS:-86400}
