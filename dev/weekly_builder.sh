#!/bin/bash
# This is the build and release script that runs with a weekly cadence (default in these scripts)
# on any-distro VPS or otherwise system where the root user is accessible and provided that
# there's a Gentoo stage3 chroot with the required packages available (default: /matrixos).
# To install such packages you can either:
# Option (A):
# - add the matrixos overlay available at https://github.com/lxnay/matrixos-overlay
# - install virtual/matrixos-devel
# - clone this git repository into /matrixos
# Option (B):
# - download a matrixOS Bedrock image or stage4 files (whichever will be available)
# - unpack the image file into a directory (you don't need boot or efi partitions)
# Option (B) advantages:
# - you get all you need (please make sure to change the passwords)
# - you can keep it up to date via ostree upgrades (see README.md)
set -e

if [ -e /etc/profile ]; then
    source /etc/profile
fi

set -eu

if [ -z "${MATRIXOS_DEV_DIR:-}" ]; then
    MATRIXOS_DEV_DIR="$(realpath $(dirname "${0}")/../)"
fi
source "${MATRIXOS_DEV_DIR}/headers/env.include.sh"
export MATRIXOS_DEV_DIR

LOGFILE=
BUILT_SEEDERS_FILE=
BUILT_RELEASES_FILE=

ARG_POSITIONALS=()
ARG_FORCE_RELEASE=
ARG_ONLY_IMAGES=
ARG_FORCE_IMAGES=
ARG_SKIP_IMAGES=
ARG_ON_BUILD_SERVER=
ARG_RESUME_SEEDERS=
ARG_SEEDER_ARGS=()
ARG_RELEASER_ARGS=()
ARG_RUN_JANITOR=1  # default.
ARG_CDN_PUSHER=
ARG_HELP=


finish() {
    exit_code=$?
    if [ -n "${ARG_HELP}" ]; then
        return "${exit_code}"
    fi
    local subject=
    if [ "${exit_code}" -eq "0" ]; then
        subject="[matrixOS weekly builder] SUCCESSFUL execution at $(date +%Y%m%d)"
    else
        subject="[matrixOS weekly builder] FAILED execution at $(date +%Y%m%d)"
    fi

    local mail_dest=
    mail_dest="$(id -n -u)"
    local mutt_exec=
    mutt_exec=$(command -v mutt 2>/dev/null || true)
    if [ -n "${mutt_exec}" ]; then
        local mutt_args=()
        if [ -n "${LOGFILE}" ] && [ -f "${LOGFILE}" ]; then
            mutt_args+=( -a "${LOGFILE}" )
        fi
        mutt -s "${subject}" "${mutt_args[@]}" -- "${mail_dest}" < /dev/null
    else
        echo "mutt not installed, not emailing ${mail_dest} with build status." >&2
    fi

    [[ -n "${BUILT_SEEDERS_FILE}" ]] && rm -f "${BUILT_SEEDERS_FILE}"
    [[ -n "${BUILT_RELEASES_FILE}" ]] && rm -f "${BUILT_RELEASES_FILE}"
}

parse_args() {
    while [[ ${#} -gt 0 ]]; do
    case ${1} in
        -fr|--force-release)
        ARG_FORCE_RELEASE=1

        shift
        ;;

        -oi|--only-images)
        ARG_ONLY_IMAGES=1

        shift
        ;;

        -fi|--force-images)
        ARG_FORCE_IMAGES=1

        shift
        ;;

        -si|--skip-images)
        ARG_SKIP_IMAGES=1

        shift
        ;;

        -bs|--on-build-server)
        ARG_ON_BUILD_SERVER=1

        shift
        ;;

        -rs|--resume-seeders)
        ARG_RESUME_SEEDERS=1

        shift
        ;;

        -s|--skip-seeders|--skip-seeders=*)
        local vals=
        if [[ "${1}" =~ --skip-seeders=.* ]]; then
            vals=${1/--skip-seeders=/}
            shift

        else
            vals="${2}"
            shift 2
        fi
        local skip_seeders=()
        readarray -d ',' -t skip_seeders <<< "${vals}"
        # Important: readarray keeps the delimiter unless you trim it.
        skip_seeders[-1]="${skip_seeders[-1]%$'\n'}"
        ARG_SEEDER_ARGS+=(
            "--skip-seeders=${skip_seeders[@]}"
        )
        ARG_RELEASER_ARGS+=(
            "--skip-seeders=${skip_seeders[@]}"
        )
        ;;

        -o|--only-seeders|--only-seeders=*)
        local vals=
        if [[ "${1}" =~ --only-seeders=.* ]]; then
            vals=${1/--only-seeders=/}
            shift
        else
            vals="${2}"
            shift 2
        fi
        local only_seeders=()
        readarray -d ',' -t only_seeders <<< "${vals}"
        # Important: readarray keeps the delimiter unless you trim it.
        only_seeders[-1]="${only_seeders[-1]%$'\n'}"
        ARG_SEEDER_ARGS+=(
            "--only-seeders=${only_seeders[@]}"
        )
        ARG_RELEASER_ARGS+=(
            "--only-seeders=${only_seeders[@]}"
        )
        ;;

        -dj|--disable-janitor)
        ARG_RUN_JANITOR=

        shift
        ;;

        -cp|--cdn-pusher|--cdn-pusher=*)
        local val=
        if [[ "${1}" =~ --cdn-pusher=.* ]]; then
            val=${1/--cdn-pusher=/}
            shift
        else
            val="${2}"
            shift 2
        fi
        ARG_CDN_PUSHER="${val}"
        ;;

        -h|--help)
        echo -e "release - matrixOS chroot release tool." >&2
        echo >&2
        echo -e "Arguments:" >&2
        echo -e "-fr, --force-release \t\t force the re-release of the latest built seeds." >&2
        echo -e "-oi, --only-images  \t\t generate the images from the last committed branches, skipping seeder and releaser." >&2
        echo -e "-fi, --force-images  \t\t force images creation for all branches, after the seeder and releaser executed." >&2
        echo -e "-si, --skip-images  \t\t skip images generation for all branches, after the seeder and releaser executed." >&2
        echo -e "-bs, --on-build-server  \t optimize execution if seeding, release and imaging happens on the same machine." >&2
        echo -e "-rs, --resume-seeders \t\t allow seeder to resume seeds (chroots) build from a checkpoint." >&2
        echo -e "-s, --skip-seeders  \t\t comma separated list of seeders to skip (by name)." >&2
        echo -e "\t\t\t\t\t Example: (00-bedrock,01-server)." >&2
        echo -e "-o, --only-seeders  \t\t comma separated allow-list of seeders to accept (by name)." >&2
        echo -e "\t\t\t\t\t Example: (00-bedrock,01-server)." >&2
        echo -e "-dj, --disable-janitor  \t disable old artifacts cleanup at the end of the build (default is enabled)." >&2
        echo -e "-cp, --cdn-pusher  \t\t hook to an executable that pushes the generated artifacts to a CDN." >&2
        echo -e "               \t\t\t\t exported vars: MATRIXOS_BUILT_RELEASES='rel1 rel2 rel3' MATRIXOS_BUILT_IMAGES='<0|1>'" >&2
        echo >&2
        ARG_HELP=1
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

_only_images_flag() {
    [[ -n "${ARG_ONLY_IMAGES}" ]]
}

_force_release_flag() {
    [[ -n "${ARG_FORCE_RELEASE}" ]]
}

_force_images_flag() {
    [[ -n "${ARG_FORCE_IMAGES}" ]]
}

_skip_images_flag() {
    [[ -n "${ARG_SKIP_IMAGES}" ]]
}

_on_build_server_flag() {
    [[ -n "${ARG_ON_BUILD_SERVER}" ]]
}

_resume_seeders_flag() {
    [[ -n "${ARG_RESUME_SEEDERS}" ]]
}

_run_janitor_flag() {
    [[ -n "${ARG_RUN_JANITOR}" ]]
}

_cdn_pusher_flag() {
    echo "${ARG_CDN_PUSHER}"
}


main() {
    trap finish EXIT

    parse_args "${@}"

    local found_uid
    found_uid=$(id -u)
    if [ "${found_uid}" != "0" ]; then
        echo "Run this as root." >&2
        exit 1
    fi

    local locks_dir="${MATRIXOS_LOCKS_DIR}/weekly-builder"
    mkdir -p "${locks_dir}"
    local lock_file="${locks_dir}/weekly-builder.lock"

    exec 9> "${lock_file}"
    flock -x -w 600 9
    if [ "${?}" != "0" ]; then
        echo "Failed to acquire the lock to build matrixOS weekly. Another weekly builder running?" >&2
        exit 1
    fi

    local log_dir="${MATRIXOS_LOGS_DIR}/weekly-builder"
    mkdir -p "${log_dir}"
    LOGFILE="${log_dir}/build-$(date +%Y%m%d-%H%M%S).log"

    echo "Logfile at: ${LOGFILE}"
    BUILT_SEEDERS_FILE=$(mktemp -p "${locks_dir}" "matrixos.weekly.builder.seeds.done.file.XXXXXXX")
    echo "Tracking newly built seeds at: ${BUILT_SEEDERS_FILE}"
    BUILT_RELEASES_FILE=$(mktemp -p "${locks_dir}" "matrixos.weekly.builder.releases.done.file.XXXXXXX")
    echo "Tracking newly built releases at: ${BUILT_RELEASES_FILE}"

    (

        local built_releases=()
        if ! _only_images_flag "${@}"; then
            local seeder_args=(
                --verbose
                --built-seeders-file="${BUILT_SEEDERS_FILE}"
                "${ARG_SEEDER_ARGS[@]}"
            )
            if _resume_seeders_flag "${@}"; then
                seeder_args+=(
                    --resume
                )
            fi
            echo "Building new seeds ..."
            "${MATRIXOS_DEV_DIR}/build/seeder" "${seeder_args[@]}"

            local releaser_args=(
                --verbose
                "${ARG_RELEASER_ARGS[@]}"
            )
            if [ ! -e "${BUILT_SEEDERS_FILE}" ]; then
                echo "Apparently, ${BUILT_SEEDERS_FILE} disappeared. Running the releaser regardless ..." >&2
            else
                built_seeds=( $(cat "${BUILT_SEEDERS_FILE}") )
                for s in "${built_seeds[@]}"; do
                    echo "Seeder built: ${s}"
                done

                if [[ "${#built_seeds[@]}" -gt 0 ]]; then
                    printf -v joined ",%s" "${built_seeds[@]}"
                    releaser_args+=(
                        "--only-seeders=${joined:1}"
                    )
                    echo "Releasing only for freshly built seeders: ${joined:1} ..."
                elif _force_release_flag "${@}"; then
                    echo "Forcing releases and new images via --force-release."
                    # fall through.
                else
                    echo "Nothing to release. Yay."
                    exit 0
                fi
            fi

            echo "Releasing newly built seeds ..."
            "${MATRIXOS_DEV_DIR}/release/release.seeds" "${releaser_args[@]}" \
                --built-releases-file="${BUILT_RELEASES_FILE}"

            echo "Creating images for the new releases ..."
            if [ ! -e "${BUILT_RELEASES_FILE}" ]; then
                echo "Unable to find ${BUILT_RELEASES_FILE}. Was it deleted?" >&2
                return 1
            fi

            built_releases+=( $(cat "${BUILT_RELEASES_FILE}") )

            if _on_build_server_flag "${@}"; then
                echo "Executing on a build server, branches are stored without remote names, so stripping that off ..."
                built_releases=( $(for r in "${built_releases[@]}"; do echo "${r#*:}"; done) )
            fi

            local r=
            for r in "${built_releases[@]}"; do
                echo "Built release: ${r}"
            done

        else
            echo "Forcing new images only via --only-images ..."
        fi

        local imager_args=(
            --local-ostree
            --productionize
            --create-qcow2
        )
        local execute_imager=
        if [[ "${#built_releases[@]}" -gt 0 ]]; then
            printf -v reljoined ",%s" "${built_releases[@]}"
            imager_args+=(
                "--only-releases=${reljoined:1}"
            )
            echo "Creating new images only for freshly built releases: ${reljoined:1} ..."
            execute_imager=1
        elif _force_images_flag "${@}"; then
            echo "Forcing new images via --force-images."
            execute_imager=1
        elif _only_images_flag "${@}"; then
            echo "Creating only images (all) via --only-images."
            execute_imager=1
        elif _skip_images_flag "${@}"; then
            echo "Skipping images creation via --skip-images."
        else
            echo "No images to release. Yay?"
        fi

        if [ -n "${execute_imager}" ]; then
            "${MATRIXOS_DEV_DIR}/image/image.releases" "${imager_args[@]}"
        fi

        if _run_janitor_flag "${@}"; then
            echo "Running janitor clean ups ..."
            "${MATRIXOS_DEV_DIR}"/dev/clean_old_builds.sh
            "${MATRIXOS_DEV_DIR}"/dev/janitor/run.sh
        fi

        local cdn_pusher=
        cdn_pusher=$(_cdn_pusher_flag "${@}")
        if [ -n "${cdn_pusher}" ] && [ -x "${cdn_pusher}" ]; then
            (
                export MATRIXOS_BUILT_RELEASES="${built_releases[*]}"
                export MATRIXOS_BUILT_IMAGES="${execute_imager:-0}"
                "${cdn_pusher}"
            )
        elif [ -n "${cdn_pusher}" ] && [ ! -x "${cdn_pusher}" ]; then
            echo "ERROR: unable to push to CDN. ${cdn_pusher} not executable!" >&2
            return 1
        fi

    ) > >(tee -a "${LOGFILE}") 2> >(tee -a "${LOGFILE}" >&2)
}

main "${@}"
