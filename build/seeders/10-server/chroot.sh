#!/bin/bash
# Actual entry point for execution inside chroot. This file is only executed inside the
# seeding chroot.
set -eu

source "${MATRIXOS_DEV_DIR:-/matrixos}/headers/env.include.sh"
source "${MATRIXOS_DEV_DIR}/build/seeders/headers/seedersenv.include.sh"

source "${MATRIXOS_DEV_DIR}/build/seeders/lib/chroots_lib.sh"

# TODO: maybe we can infer the kernel from the package list.
BUILD_KERNEL_PACKAGES=(
    sys-kernel/matrixos-kernel::matrixos
)
UPSTREAM_PORTAGE_REPOS=()

_seeder_name=$(basename "$(dirname "${0}")")


server.buildenv_bootstrap() {
    chroots_lib.default_buildenv_bootstrap "${_seeder_name}"
}

server.portage_bootstrap() {
    chroots_lib.default_portage_bootstrap "${UPSTREAM_PORTAGE_REPOS[@]}"
}

server.build_everything() {
    chroots_lib.default_build_everything "${_seeder_name}"
    # Trigger a rebuild of the kernel so that we bundle the latest and
    # correct initramfs setup.
    chroots_lib.generic_forced_rebuild "${BUILD_KERNEL_PACKAGES[@]}"
}

server.clean_temporary_artifacts() {
    chroots_lib.default_clean_temporary_artifacts
}

server.tweak_nsswitch() {
    # make the default /etc/nsswitch.conf a bit less dumb
    # and add support for dns and mdns resolution.
    # This is done here because it's tied to the portage setup.
    sed -i '/^hosts:/c\hosts:      files myhostname mymachines dns mdns_minimal [NOTFOUND=return] resolve [!UNAVAIL=return]' \
        "/etc/nsswitch.conf"
}

server.tweak_resolved() {
    # Disable multicast DNS support in systemd-resolved as atm
    # avahi-daemon is providing it.
    local resolved_conf="/etc/systemd/resolved.conf"
    if [ -f "${resolved_conf}" ]; then
        echo "# matrixOS uses avahi for Multicast DNS." >> "${resolved_conf}"
        echo "MulticastDNS=no" >> "${resolved_conf}"
    fi
}

main() {

    local phases=(
        server.buildenv_bootstrap
        server.portage_bootstrap
        server.build_everything
        server.tweak_nsswitch
        server.clean_temporary_artifacts
        server.tweak_resolved
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