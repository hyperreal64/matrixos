#!/bin/bash
# build.sh is a script that allows you to build a matrixOS image yourself, using the configs
# in this repository. It's basically a BYOD (Build Your Own Distro) script for the best
# Linux distribution out there, Gentoo Linux.
# This script is a wrapper around weekly_builder.sh that helps with the provisioning of important
# private keys and configs.
set -e

if [ -e /etc/profile ]; then
    source /etc/profile
fi

set -eu

if [ -z "${MATRIXOS_DEV_DIR:-}" ]; then
    MATRIXOS_DEV_DIR="$(realpath $(dirname "${0}")/../)"
fi
source "${MATRIXOS_DEV_DIR}"/headers/env.include.sh
export MATRIXOS_DEV_DIR

source "${MATRIXOS_DEV_DIR}/lib/ostree_lib.sh"
source "${MATRIXOS_DEV_DIR}/lib/qa_lib.sh"
source "${MATRIXOS_DEV_DIR}/build/seeders/lib/seeders_lib.sh"

_is_help_arg() {
    local arg="${1:-}"
    case "${arg}" in
        -h|--help)
            return 0
            ;;
        *)
            return 1
            ;;
    esac
}

_is_help_in_args() {
    for arg in "${@}"; do
        if _is_help_arg "${arg}"; then
            return 0
        fi
    done
    return 1
}

_print_build_warning() {
    echo "ATTENTION PLEASE"
    echo "Using Git repo: ${MATRIXOS_GIT_REPO} (from conf/matrixos.conf)"
    echo "If you want to make changes to the build configs, it's preferred to fork the official repo"
    echo "and > edit conf/matrixos.conf GitRepo parameter, setting the URL to your ${MATRIXOS_OSNAME}.git repo fork."
    echo
    echo "Alternatively, use UseLocalGitRepoInsideChroot and optionally also DeleteDotGitFromGitRepo conf/matrixos.conf"
    echo "settings, to do a local clone of the git repository inside the chroot for bootstrapping."
    echo "In both cases, the repo will be cloned inside seed chroots via git clone. This means that all uncommitted"
    echo "changes will NOT be picked up by the build process. The build will start in 5 seconds ..."
    echo
    sleep 5
}

main() {

    if ! _is_help_in_args "${@}"; then
        qa_lib.root_privs
        _print_build_warning
        ostree_lib.setup_environment
    fi

    seeders_lib.maybe_initialize_matrixos_private_example "${MATRIXOS_PRIVATE_GIT_REPO_PATH}"

    exec "${MATRIXOS_DEV_DIR}/dev/weekly_builder.sh" --on-build-server --disable-send-mail "${@}"
}

main "${@}"
