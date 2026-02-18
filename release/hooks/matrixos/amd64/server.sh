#!/bin/bash
# server.sh (hook) - Execute a customization script for QA or other CI/CD systems
#                    to consume right before committing the root filesystem (CHROOT_DIR)
#                    to the local ostree repository. This hook should return non-zero exit
#                    status in case of issues. Warnings must be logged to stderr.
#
# These are the env variables that are made available:
#
#
# CHROOT_DIR=/path/to/chroot
# The directory path to a the root filesystem ready to be committed to ostree.
set -e
source "${MATRIXOS_DEV_DIR}/headers/env.include.sh"


setup_networkd() {
    local imagedir="${1}"
    echo "Setting up systemd-networkd configuration for DHCP in ${imagedir}"

    local networkd_dir="${imagedir}/etc/systemd/network"
    mkdir -p "${networkd_dir}"
    cat > "${networkd_dir}/20-matrixos-wired.network" << EOF
[Match]
Type=ether

[Network]
DHCP=yes
EOF
}

main() {
    setup_networkd "${CHROOT_DIR}"
}

main "${@}"

