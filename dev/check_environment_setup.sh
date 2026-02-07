#!/bin/bash
# Script to check if an Linux based OS has all the required binaries to run
# the matrixOS build, release and image binaries (i.e. the whole workflow).
set -eu

if [ -z "${MATRIXOS_DEV_DIR:-}" ]; then
    MATRIXOS_DEV_DIR="$(realpath $(dirname "${0}")/../)"
fi
source "${MATRIXOS_DEV_DIR}"/headers/env.include.sh

source "${MATRIXOS_DEV_DIR}"/lib/qa_lib.sh

echo "If the following lines contain <something> not found, you should install the respective package."
echo

echo "Checking for seeders support (to build root filesystem from gentoo stage3):"
qa_lib.verify_seeder_environment_setup "/" || true
echo

echo "Checking for releaser support (to release chroots to ostree):"
qa_lib.verify_releaser_environment_setup "/" || true
echo

echo "Checking for imager support (to create bootable images [WITH GPG DEFAULTS]):"
qa_lib.verify_imager_environment_setup "/" "${MATRIXOS_OSTREE_GPG_ENABLED}" || true

echo "Checking for imager support (to create bootable images [WITHOUT GPG ENABLED]):"
qa_lib.verify_imager_environment_setup "/" "" || true