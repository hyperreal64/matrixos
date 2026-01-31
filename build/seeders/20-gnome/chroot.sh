#!/bin/bash
# Actual entry point for execution inside chroot. This file is only executed inside the
# seeding chroot.
set -eu

source "${MATRIXOS_DEV_DIR:-/matrixos}/headers/env.include.sh"
source "${MATRIXOS_DEV_DIR}/build/seeders/headers/seedersenv.include.sh"

source "${MATRIXOS_DEV_DIR}/build/seeders/lib/chroots_lib.sh"


UPSTREAM_PORTAGE_REPOS=(
    steam-overlay
    guru
)

_seeder_name=$(basename "$(dirname "${0}")")


gnome.buildenv_bootstrap() {
    chroots_lib.default_buildenv_bootstrap "${_seeder_name}"
}

gnome.portage_bootstrap() {
    chroots_lib.default_portage_bootstrap "${UPSTREAM_PORTAGE_REPOS[@]}"
}

gnome.build_everything() {
    chroots_lib.default_build_everything "${_seeder_name}"
}

gnome.clean_temporary_artifacts() {
    chroots_lib.default_clean_temporary_artifacts

    # Clean stale distfiles
    eclean-dist
    eclean-pkg
}

gnome.tweak_nsswitch() {
    # make the default /etc/nsswitch.conf a bit less dumb
    # and add support for dns and mdns resolution.
    # This is done here because it's tied to the portage setup.
    sed -i '/^hosts:/c\hosts:      files myhostname mymachines dns mdns_minimal [NOTFOUND=return] resolve [!UNAVAIL=return]' \
        "/etc/nsswitch.conf"
}

main() {

    local phases=(
        gnome.buildenv_bootstrap
        gnome.portage_bootstrap
        gnome.build_everything
        gnome.tweak_nsswitch
        gnome.clean_temporary_artifacts
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