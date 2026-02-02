#!/bin/bash
set -eu

if [ -z "${MATRIXOS_DEV_DIR:-}" ]; then
    MATRIXOS_DEV_DIR="$(realpath $(dirname "${0}")/../)"
fi
source "${MATRIXOS_DEV_DIR}"/headers/env.include.sh

source "${MATRIXOS_DEV_DIR}"/release/lib/release_lib.sh
source "${MATRIXOS_DEV_DIR}"/lib/ostree_lib.sh
source "${MATRIXOS_DEV_DIR}"/lib/fs_lib.sh
source "${MATRIXOS_DEV_DIR}"/lib/qa_lib.sh


ARG_POSITIONALS=()
ARG_OSTREE_BRANCH=
ARG_GPG_ENABLED="${MATRIXOS_OSTREE_GPG_ENABLED}"
ARG_CHROOT_DIR=
ARG_IMAGE_DIR=
ARG_VERBOSE_MODE=0

MOUNTS=()

clean_exit() {
    fs_lib.cleanup_mounts "${MOUNTS[@]}"
}

parse_args() {
    while [[ ${#} -gt 0 ]]; do
    case ${1} in
        -b|--branch|--branch=*)
        # check if we have a --branch
        local val=
        if [[ "${1}" =~ --branch=.* ]]; then
            val=${1/--branch=/}
            shift
        else
            val="${2}"
            shift 2
        fi
        if [ -z "${val}" ]; then
            echo "${0}: invalid branch flag." >&2
            return 1
        fi

        if ostree_lib.is_branch_full_suffixed "${val}"; then
            echo "${0}: do not pass -${MATRIXOS_OSTREE_FULL_SUFFIX} suffixed branch names. Just plain branch." >&2
            return 1
        fi

        if ostree_lib.is_branch_shortname "${val}"; then
            # assume dev to be safe.
            echo "${0}: WARNING: branch shortname specified, assuming dev release stage." >&2
            val=$(ostree_lib.branch_shortname_to_normal "dev" "${val}")
        fi
        ARG_OSTREE_BRANCH="${val}"
        ;;

        -d|--chroot-dir|--chroot-dir=*)
        # check if we have a --chroot-dir=
        local val=
        if [[ "${1}" =~ --chroot-dir=.* ]]; then
            val=${1/--chroot-dir=/}
            shift
        else
            val="${2}"
            shift 2
        fi
        ARG_CHROOT_DIR="${val}"
        ;;

        -i|--image-dir|--image-dir=*)
        # check if we have a --image-dir=
        local val=
        if [[ "${1}" =~ --image-dir=.* ]]; then
            val=${1/--image-dir=/}
            shift
        else
            val="${2}"
            shift 2
        fi
        ARG_IMAGE_DIR="${val}"
        ;;

        -dgpg|--disable-gpg)
        ARG_GPG_ENABLED=""

        shift
        ;;

        -v|--verbose)
        ARG_VERBOSE_MODE=1

        shift
        ;;

        -h|--help)
        echo -e "release - matrixOS chroot release tool." >&2
        echo >&2
        echo -e "Arguments:" >&2
        echo -e "-b, --branch \t\t set the OSTree branch short name to work on (default: stable)." >&2
        echo -e "-d, --chroot-dir  \t\t\t\t override the default inferred chroot dir." >&2
        echo -e "-i, --image-dir  \t\t\t\t override the default inferred image dir." >&2
        echo -e "-dgpg, --disable-gpg  \t\t\t\t force disable gpg support." >&2
        echo -e "-v, --verbose \t\t enable verbose mode (default: false)." >&2
        echo >&2
        exit 0
        ;;

        -*|--*)
        echo "Unknown argument ${1}"
        return 1
        ;;
        *)
        ARG_POSITIONALS+=( "${1}" )
        shift
        ;;
    esac
    done
}

main() {
    trap clean_exit EXIT

    parse_args "${@}"
    qa_lib.root_privs

    if [ -z "${ARG_CHROOT_DIR}" ]; then
        echo "--chroot-dir= unset. Unable to proceed." >&2
        return 1
    fi
    if [ -z "${ARG_IMAGE_DIR}" ]; then
        echo "--image-dir= unset. Unable to proceed." >&2
        return 1
    fi
    if [ -z "${ARG_OSTREE_BRANCH}" ]; then
        echo "--branch= unset. Unable to proceed." >&2
        return 1
    fi

    local gpg_enabled="${ARG_GPG_ENABLED}"
    local branch="${ARG_OSTREE_BRANCH}"
    local full_branch
    full_branch="$(ostree_lib.branch_to_full "${branch}")"

    qa_lib.verify_releaser_environment_setup "/"
    release_lib.check_matrixos
    release_lib.sync_filesystem "${ARG_CHROOT_DIR}" "${ARG_IMAGE_DIR}" "${ARG_VERBOSE_MODE}"
    release_lib.sync_matrixos_dir "${ARG_IMAGE_DIR}"
    release_lib.pre_clean_qa_checks "${ARG_IMAGE_DIR}"
    release_lib.clean_rootfs "${ARG_IMAGE_DIR}"
    release_lib.setup_services "${ARG_IMAGE_DIR}" "MOUNTS" "${branch}"
    release_lib.setup_hostname "${ARG_IMAGE_DIR}"
    release_lib.post_clean_qa_checks "${ARG_IMAGE_DIR}"
    release_lib.initialize_gpg "${gpg_enabled}"

    release_lib.release_hook "${ARG_IMAGE_DIR}" "${branch}"
    release_lib.ostree_prepare "${ARG_IMAGE_DIR}"
    release_lib.maybe_ostree_init "${MATRIXOS_OSTREE_REPO_DIR}"

    # Remove /etc symlink before commit.
    release_lib.unlink_etc "${ARG_IMAGE_DIR}"
    # Full tree.
    release_lib.release \
        "${MATRIXOS_OSTREE_REPO_DIR}" \
        "${ARG_IMAGE_DIR}" \
        "${ARG_VERBOSE_MODE}" \
        "${gpg_enabled}" \
        "${full_branch}" \
        "" \
        "0"

    # In post_clean_shrink we use emerge again, so fix /etc and /etc/portage temporarily.
    release_lib.symlink_etc "${ARG_IMAGE_DIR}"
    release_lib.add_extra_dotdot_to_usr_etc_portage "${ARG_IMAGE_DIR}"

    # Clean up unnecessary dev-related data.
    release_lib.post_clean_shrink "${ARG_IMAGE_DIR}" "MOUNTS" "${branch}"
    # Remove extra ../ from /etc/portage symlink so that when it gets deployed client side,
    # the symlink is valid.
    release_lib.remove_extra_dotdot_from_usr_etc_portage "${ARG_IMAGE_DIR}"

    # Remove /etc symlink before commit.
    release_lib.unlink_etc "${ARG_IMAGE_DIR}"
    # Commit to the smaller branch.
    release_lib.release \
        "${MATRIXOS_OSTREE_REPO_DIR}" \
        "${ARG_IMAGE_DIR}" \
        "${ARG_VERBOSE_MODE}" \
        "${gpg_enabled}" \
        "${branch}" \
        "${full_branch}" \
        "1"

    echo "Committed image at ${ARG_IMAGE_DIR} to ostree at ${MATRIXOS_OSTREE_REPO_DIR}."
}

main "${@}"
