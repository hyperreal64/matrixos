#!/bin/bash
# clean_old_builds.sh -- cleans old chroot builds from the chroots dir.
set -e

if [ -e /etc/profile ]; then
    source /etc/profile
fi

set -eu

source "${MATRIXOS_DEV_DIR:-/matrixos}"/headers/env.include.sh
source "${MATRIXOS_DEV_DIR}"/build/seeders/headers/seedersenv.include.sh

source "${MATRIXOS_DEV_DIR}"/build/seeders/lib/seeders_lib.sh
source "${MATRIXOS_DEV_DIR}"/lib/fs_lib.sh

HOW_MANY_OLD_RELEASES=1

main() {
    local seeders=
    seeders=( $(seeders_lib.detect_seeders 'false' 'true' 2>/dev/null) )

    local chroot_dir=
    local seeder_exec=
    local marked_for_removal=()
    for seeder_exec in "${seeders[@]}"; do
        echo "Detected seeder executable: ${seeder_exec}"
        local seeder_dir_name=
        seeder_name=$(seeders_lib.seeder_exec_to_name "${seeder_exec}")
        local seeder_clean_name=
        seeder_clean_name=$(seeders_lib.seeder_name_without_order_prefix "${seeder_name}")

        local seeder_dir=
        seeder_dir=$(dirname "${seeder_exec}")

        local params=
        params="${seeder_dir}/${MATRIXOS_SEEDER_PARAMS_EXEC_NAME}"
        if [ -f "${params}" ]; then
            source "${params}"

            local latest_chroot_dir=
            latest_chroot_dir=$("${seeder_clean_name}_params.find_latest_chroot_dir" "${seeder_name}")
            if [ -z "${latest_chroot_dir}" ]; then
                echo "[${seeder_name}] Unable to find latest chroot dir for in ${seeder_dir} ... Skipping." >&2
                continue
            fi
            echo "[${seeder_name}] Found latest chroot dir ${latest_chroot_dir}"

            local all_chroot_dirs=
            all_chroot_dirs=$("${seeder_clean_name}_params.find_all_chroot_dirs" "${seeder_name}")
            if [ -z "${all_chroot_dirs}" ]; then
                echo "[${seeder_name}] Unable to find all chroot dir for in ${seeder_dir} ... Skipping." >&2
            fi
            # Turn this into an array.
            all_chroot_dirs=( ${all_chroot_dirs} )
            local old_chroot_dirs=()
            for chroot_dir in "${all_chroot_dirs[@]}"; do
                if [ "${chroot_dir}" != "${latest_chroot_dir}" ]; then
                    echo "[${seeder_name}] Detected old chroot dir: ${chroot_dir}"
                    old_chroot_dirs+=( "${chroot_dir}" )
                fi
            done

            if [[ "${#old_chroot_dirs[@]}" -gt 1 ]]; then
                echo "[${seeder_name}] Detected more than one old chroot dir. Deleting older ones ..."

                local sorted_old_chroot_dirs=()
                mapfile -t sorted_old_chroot_dirs < <(printf "%s\n" "${old_chroot_dirs[@]}" | sort -V)

                # remove the n last elements from sorted_old_chroot_dirs.
                local reduced_old_chroot_dirs=
                reduced_old_chroot_dirs=( ${sorted_old_chroot_dirs[@]:0:${#sorted_old_chroot_dirs[@]}-HOW_MANY_OLD_RELEASES} )

                for chroot_dir in "${reduced_old_chroot_dirs[@]}"; do
                    # safety check.
                    if [ "${chroot_dir}" = "${latest_chroot_dir}" ]; then
                        echo "[${seeder_name}] SAFETY CHECK: not deleting ${chroot_dir} as it's the latest. This is a bug." >&2
                        continue
                    fi
                    marked_for_removal+=( "${chroot_dir}" )
                done
            else
                echo "[${seeder_name}] Nothing to clean in ${seeder_dir} ..."
                continue
            fi
        fi
    done

    if [[ "${#marked_for_removal[@]}" -eq 0 ]]; then
        echo "Nothing to clean."
        exit 0
    fi
    echo "Marked for removal:" >&2
    for chroot_dir in "${marked_for_removal[@]}"; do
        echo "${chroot_dir}" >&2
    done
    for chroot_dir in "${marked_for_removal[@]}"; do
        if ! fs_lib.check_dir_is_root "${chroot_dir}"; then
            echo "${chroot_dir} is /! This is a bug. Not removing." >&2
            exit 1
        fi
        if ! fs_lib.check_active_mounts "${chroot_dir}"; then
            echo "${chroot_dir} has active mounts. Not removing." >&2
            continue
        fi
        echo "Removing ${chroot_dir} ..." >&2
        rm -rf "${chroot_dir}"
    done
}

main "${@}"