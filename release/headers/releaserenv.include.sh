#!/bin/bash
# This file is sourced inside release scripts.
# It contains common releaser execution variables.
set -eu

source "${MATRIXOS_DEV_DIR:-/matrixos}"/headers/env.include.sh


# The default hostname of the released ostree repo.
MATRIXOS_RELEASE_HOSTNAME=${MATRIXOS_RELEASE_HOSTNAME:-matrixos}

# In-chroot building variables. This variable is used by release.current.rootfs binary
# to create a read-only bind mount of the root filesystem before running the releasec code
# as a safety measure to avoid accidental removal of / files.
MATRIXOS_RELEASE_RO_ROOT_DIR="${MATRIXOS_RELEASE_RO_ROOT_DIR:-/tmp/.matrixos_release_rootfs}"

# MATRIXOS_RELEASE_HOOKS_DIR=/path/to/dir
# Release hooks are used by the releaser to allow for branch specific
# release customizations. For example, if you are building a GNOME image
# you may want to prep the desktop for a gnomic experience!
# The directory acts as a base path for finding the hook script, which
# is then composed together with the ostree branch name (without -full suffix).
MATRIXOS_RELEASE_HOOKS_DIR="${MATRIXOS_DEV_DIR}/release/hooks"

# MATRIXOS_RELEASER_USE_CP_REFLINK_MODE_INSTEAD_OF_RSYNC=1
# To enable the releaser to clone source chroots using cp --reflink=auto instead of using
# rsync. This effectively reduces disk space by a lot when maintaining several builds for
# filesystems that support it.
MATRIXOS_RELEASER_USE_CP_REFLINK_MODE_INSTEAD_OF_RSYNC=${MATRIXOS_RELEASER_USE_CP_REFLINK_MODE_INSTEAD_OF_RSYNC:-1}

# MATRIXOS_USE_OSTREE_COMMIT_CONSUME_FLAG=1?
# Tell ostree commit to use --consume, which will move files from the source chroot
# instead of creating hardlinks. This will preserve CoW properties of filesystems
# supporting it (like btrfs, xfs, zfs), saves space, much wow.
# ATTENTION DO NOT SET THIS TO 1 FOR ROOTFS = / COMMITS, IT WILL DESTROY YOUR SYSTEM.
MATRIXOS_USE_OSTREE_COMMIT_CONSUME_FLAG=${MATRIXOS_USE_OSTREE_COMMIT_CONSUME_FLAG:-}

# MATRIXOS_RELEASE_LOCK_DIR=/path/to/locks/dir
# Directory used by releaser to contain file locks for coordinating release chroots management.
MATRIXOS_RELEASE_LOCK_DIR="${MATRIXOS_RELEASE_LOCK_DIR:-"${MATRIXOS_LOCKS_DIR}/release"}"

# MATRIXOS_RELEASE_LOCK_WAIT_SECS=secs
# Number of seconds to wait before giving up on waiting for a release chroot file lock.
MATRIXOS_RELEASE_LOCK_WAIT_SECS=${MATRIXOS_RELEASE_LOCK_WAIT_SECS:-86400}
