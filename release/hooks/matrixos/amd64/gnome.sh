#!/bin/bash
# gnome.sh (hook) - Execute a customization script for QA or other CI/CD systems
#                   to consume right before committing the root filesystem (CHROOT_DIR)
#                   to the local ostree repository. This hook should return non-zero exit
#                   status in case of issues. Warnings must be logged to stderr.
#
# These are the env variables that are made available:
#
#
# CHROOT_DIR=/path/to/chroot
# The directory path to a the root filesystem ready to be committed to ostree.
set -e
source "${MATRIXOS_DEV_DIR}/headers/env.include.sh"

source "${MATRIXOS_DEV_DIR}/lib/fs_lib.sh"

setup_gnome_shell() {
    local imagedir="${1}"
    fs_lib.chroot "${imagedir}" \
        eselect gnome-shell-extensions enable \
            dash-to-panel@jderose9.github.com
}

setup_gnome_accounts() {
    local imagedir="${1}"

    local as_dir="${imagedir}/var/lib/AccountsService"
    local as_users_dir="${as_dir}/users"
    local as_icons_dir="${as_dir}/icons"
    if [ ! -d "${as_icons_dir}" ]; then
        mkdir -p "${as_icons_dir}"
    fi

    # In case we logged via gdm or there's a stale config.
    rm -f "${as_users_dir}/root" || true
    mkdir -p "${as_users_dir}"

    local user_cfg="${as_users_dir}/${MATRIXOS_DEFAULT_USERNAME}"
    local icon_path="/usr/share/pixmaps/faces/${MATRIXOS_OSNAME}.jpg"
    local src_icon_path="${imagedir}${icon_path}"
    if [ ! -e "${src_icon_path}" ]; then
        echo "ERROR: Image shipped without ${icon_path}." >&2
        return 1
    fi

    echo -e "\
[User]
Languages=
Session=
Icon=${icon_path}
SystemAccount=false" > "${user_cfg}"
    chmod 600 "${user_cfg}"
    chown root "${user_cfg}"
}

main() {
    setup_gnome_shell "${CHROOT_DIR}"
    setup_gnome_accounts "${CHROOT_DIR}"
}

main "${@}"