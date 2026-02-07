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

source "${MATRIXOS_DEV_DIR}/lib/qa_lib.sh"


main() {
    qa_lib.root_privs

    echo "ATTENTION PLEASE"
    echo "Using Git repo: ${MATRIXOS_GIT_REPO}"
    echo "If you want to make changes to the build configs, it's preferred to fork the official repo"
    echo "and > export MATRIXOS_GIT_REPO=<the URL to your ${MATRIXOS_OSNAME}.git repo fork>"
    echo "The repo will be cloned inside seed chroots. All uncommitted changes for building will not"
    echo "be picked up by the build process."
    echo
    sleep 5

    local private_git_url="${MATRIXOS_PRIVATE_EXAMPLE_GIT_REPO}"
    local private_repo_path="${MATRIXOS_PRIVATE_GIT_REPO_PATH}"
    if [ ! -d "${private_repo_path}" ]; then
        echo "${private_repo_path} does not exist. Pulling it from: ${private_git_url} ..." >&2
        git clone --depth 1 "${private_git_url}" "${private_repo_path}"
        (
            cd "${private_repo_path}"
            ./make.sh
        )
    elif [ ! -d "${private_repo_path}/.git" ]; then
        echo "${private_repo_path} must be a git repo" >&2
        return 1
    else
        echo "Updating ${private_repo_path} ..."
        (
            cd "${private_repo_path}"
            if [ ! -e .built ] && [ ! -e "${MATRIXOS_SECUREBOOT_KEY_PATH}" ]; then
                ./make.sh
            fi
        )
    fi

    exec "${MATRIXOS_DEV_DIR}/dev/weekly_builder.sh" --on-build-server "${@}"
}

main "${@}"
