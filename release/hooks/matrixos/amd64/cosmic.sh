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


setup_greetd() {
    local imagedir="${1}"

    local greetd_dir="${imagedir}/etc/greetd"
    if [ ! -d "${greetd_dir}" ]; then
        mkdir -p "${greetd_dir}"
    fi
    local greetd_cfg="${greetd_dir}/config.toml"
    cat > "${greetd_cfg}" << EOF
[terminal]
vt = 1

[default_session]
command = "cosmic-comp /usr/bin/cosmic-greeter"
user = "greetd"
EOF
}

main() {
    setup_greetd "${CHROOT_DIR}"
}

main "${@}"