#!/bin/bash
set -eu

source "${MATRIXOS_DEV_DIR:-/matrixos}/headers/env.include.sh"
source "${MATRIXOS_DEV_DIR}/build/seeders/headers/seedersenv.include.sh"

source "${MATRIXOS_DEV_DIR}/build/seeders/lib/chroots_lib.sh"


BOOTSTRAP_PACKAGES=(
    app-eselect/eselect-repository
    dev-vcs/git
)
# TODO: maybe we can infer the kernel from the package list.
BUILD_KERNEL_PACKAGES=(
    sys-kernel/matrixos-kernel::matrixos
)
UPSTREAM_PORTAGE_REPOS=(
    matrixos
)

# Used by main() to get the initial portage counter value, to then
# determine which package was already (re)built and which not.
INITIAL_PORTAGE_COUNTER="0"

_seeder_name=$(basename "$(dirname "${0}")")


bedrock.system_bootstrap() {
    locale-gen
    emerge-webrsync

    local common_args
    read -ra common_args <<< "$(chroots_lib.emerge_common_args)"
    # special bootstrapping command, skipping env-update.
    emerge "${common_args[@]}" "${BOOTSTRAP_PACKAGES[@]}"
}

bedrock.buildenv_bootstrap() {
    chroots_lib.default_buildenv_bootstrap "${_seeder_name}"

    if [ "${INITIAL_PORTAGE_COUNTER}" = "0" ]; then
        echo "Initial Portage Counter is 0, this is unexpected." >&2
        return 1
    fi
}

bedrock._setup_matrixos_overlay() {
    if [ -z "${MATRIXOS_OVERLAY_GIT_REPO}" ]; then
        echo "bedrock._setup_matrixos_overlay: mssing parameter to setup_matrixos_overlay" >&2
        return 1
    fi

    # matrixos is not in repositories.xml yet.
    echo "Preparing matrixos overlay ..."
    eselect repository disable -f matrixos
    eselect repository add matrixos git "${MATRIXOS_OVERLAY_GIT_REPO}"
}

bedrock.portage_bootstrap() {
    bedrock._setup_matrixos_overlay
    chroots_lib.default_portage_bootstrap "${UPSTREAM_PORTAGE_REPOS[@]}"
}

bedrock.build_resolve_conflicts() {
    # Break circular dependencies
    USE="-gpm" chroots_lib.generic_build -1 sys-libs/ncurses:0
    USE="-sysprof -avif -truetype" chroots_lib.generic_build -1 dev-libs/glib:2
    chroots_lib.generic_build -1 dev-libs/glib
}

bedrock.build_kernel() {
    chroots_lib.generic_build "${BUILD_KERNEL_PACKAGES[@]}"
}

bedrock.build_system() {
    # Rebuild for new use flags pulling in dependencies and build deps.
    local flags=(
        --newuse
        --deep
        --with-bdeps=y
    )
    local packages=(
        @system
    )
    chroots_lib.generic_build "${flags[@]}" "${packages[@]}"
}

bedrock.build_everything() {
    chroots_lib.default_build_everything "${_seeder_name}"

    echo "Initial portage counter was: ${INITIAL_PORTAGE_COUNTER}"
    echo "Packages with a counter greater than this, were built with matrixOS setup."
    echo "Packages with a counter lower than this need to be rebuilt. Rebuilds:"
    chroots_lib.rebuild_before_portage_counter "${INITIAL_PORTAGE_COUNTER}"
}

bedrock.clean_temporary_artifacts () {
    chroots_lib.default_clean_temporary_artifacts
}

setup_portage_counter() {
    # Load portage counter from disk if we previously saved it.
    local stored_portage_counter=
    stored_portage_counter=$(chroots_lib.get_stored_portage_counter)
    if [ "${stored_portage_counter}" = "-1" ]; then
        # does not exist, initialize.
        local current_counter=
        current_counter=$(chroots_lib.get_current_portage_counter)
        echo "Initializing Portage counter at: ${current_counter}"
        chroots_lib.store_portage_counter "${current_counter}"
        INITIAL_PORTAGE_COUNTER="${current_counter}"
    else
        echo "Loaded Portage counter: ${stored_portage_counter}"
        INITIAL_PORTAGE_COUNTER="${stored_portage_counter}"
    fi
}

bedrock.tweak_nsswitch() {
    # make the default /etc/nsswitch.conf a bit less dumb
    # and add support for dns and mdns resolution.
    # This is done here because it's tied to the portage setup.
    sed -i '/^hosts:/c\hosts:      files myhostname mymachines dns mdns_minimal [NOTFOUND=return] resolve [!UNAVAIL=return]' \
        "/etc/nsswitch.conf"
}

main() {
    cd /
    setup_portage_counter

    local phases=(
        bedrock.system_bootstrap
        bedrock.buildenv_bootstrap
        bedrock.portage_bootstrap
        bedrock.build_resolve_conflicts
        bedrock.build_kernel
        bedrock.build_system
        bedrock.build_everything
        bedrock.tweak_nsswitch
        bedrock.clean_temporary_artifacts
    )

    # Pre-run tests to check that for every phase we have a function declared
    for phase in "${phases[@]}"; do
        if ! declare -F "${phase}"; then
            echo "Function ${phase} does not exist." >&2
            return 1
        fi
    done

    for phase_f in "${phases[@]}"; do
        if ! chroots_lib.is_phase_done "${phase_f}"; then
            echo "Executing phase: ${phase_f} ..."
            "${phase_f}"
            chroots_lib.touch_done_phase "${phase_f}"
        else
            echo "${phase_f} already finished."
        fi
    done
}

main "${@}"