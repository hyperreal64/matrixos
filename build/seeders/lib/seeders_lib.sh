#!/bin/bash
# seeders_lib.sh - shared library used to manage seeders before, during and after their execution.
#                  From outside their root fs.
set -eu

source "${MATRIXOS_DEV_DIR:-/matrixos}"/headers/env.include.sh
source "${MATRIXOS_DEV_DIR}/build/seeders/headers/preppersenv.include.sh"
source "${MATRIXOS_DEV_DIR}/build/seeders/headers/seedersenv.include.sh"


seeders_lib.retryable_cmd() {
    local tries="${1}"
    local cmd="${2}"
    local count=0
    shift 2
    until "${cmd}" "${@}"; do
        ((count++))
        if [[ "$count" -ge "${tries}" ]]; then
            echo "Command failed after ${tries} attempts." >&2
            return 1
        fi
        echo "Attempt ${count}/${tries} failed! Retrying in 5 seconds..." >&2
        sleep 5
    done
}

seeders_lib.seeders_dir() {
    local seeders_dir="${MATRIXOS_DEV_DIR}/build/seeders"
    if [ ! -d "${seeders_dir}" ]; then
        echo "${seeders_dir} seeders dir is not a directory." >&2
        return 1
    fi
    echo "${seeders_dir}"
}

seeders_lib.detect_seeders() {
    local skip_seeder_check_func="${1}"
    local only_seeder_check_func="${2}"
    if [ -z "${skip_seeder_check_func}" ]; then
        echo "Missing skip_seeder_check_func parameter to detect_seeders" >&2
        return 1
    fi
    if [ -z "${only_seeder_check_func}" ]; then
        echo "Missing only_seeder_check_func parameter to detect_seeders" >&2
        return 1
    fi

    local seeders_dir
    seeders_dir=$(seeders_lib.seeders_dir)
    local seeder_dir=
    local seeder_exec=
    for seeder_dir in "${seeders_dir}"/*/; do
        [[ -d "${seeder_dir}" ]] || continue
        seeder_dir="${seeder_dir%/}" # Remove trailing slash

        local disabled="${seeder_dir}/${MATRIXOS_DISABLED_SEEDER_FILE}"
        if [ -e "${disabled}" ]; then
            echo "Skipping disabled seeder in: ${seeder_dir}" >&2
            continue
        fi

        seeder_exec="${seeder_dir}/${MATRIXOS_SEEDER_CHROOT_EXEC_NAME}"
        prepper_exec="${seeder_dir}/${MATRIXOS_SEEDER_PREPPER_EXEC_NAME}"

        [[ -e "${seeder_exec}" ]] || continue

        local seeder_name
        seeder_name=$(seeders_lib.seeder_exec_to_name "${seeder_exec}")
        if ${skip_seeder_check_func} "${seeder_name}"; then
            echo "Skipping seeder: ${seeder_name} as requested by flags." >&2
            continue
        fi
        if ! ${only_seeder_check_func} "${seeder_name}"; then
            echo "Skipping seeder: ${seeder_name} not in list of seeders to execute." >&2
            continue
        fi

        if [ ! -x "${seeder_exec}" ]; then
            echo "Please chmod +x ${seeder_exec}" >&2
            return 1
        fi

        if [ ! -e "${prepper_exec}" ]; then
            echo "${prepper_exec} does not exist." >&2
            return 1
        fi
        if [ ! -x "${prepper_exec}" ]; then
            echo "Please chmod +x ${prepper_exec}" >&2
            return 1
        fi

        echo "Found seeder at: ${seeder_exec}" >&2
        echo "Found prepper at: ${prepper_exec}" >&2

        echo "${seeder_exec}"
    done
}

seeders_lib.seeder_exec_to_dir() {
    local seeder_exec="${1}"
    if [ -z "${seeder_exec}" ]; then
        echo "Missing parameter to seeder_exec_to_name" >&2
        return 1
    fi
    local seeder_dir
    seeder_dir=$(dirname "${seeder_exec}")
    echo "${seeder_dir}"
}

seeders_lib.seeder_exec_to_name() {
    local seeder_exec="${1}"
    if [ -z "${seeder_exec}" ]; then
        echo "Missing parameter to seeder_exec_to_name" >&2
        return 1
    fi
    local seeder_dir
    seeder_dir=$(seeders_lib.seeder_exec_to_dir "${seeder_exec}")
    local seeder_name
    seeder_name=$(basename "${seeder_dir}")
    echo "${seeder_name}"
}

seeders_lib.seeder_name_without_order_prefix() {
    local seeder_name="${1}"
    if [ -z "${seeder_name}" ]; then
        echo "Missing parameter to seeder_name_without_order_prefix" >&2
        return 1
    fi
    echo "${seeder_name#*-}"
}

seeders_lib.seeder_chroot_dir_to_name() {
    local chroot_dir="${1}"
    if [ -z "${chroot_dir}" ]; then
        echo "Missing parameter to seeder_chroot_dir_to_name" >&2
        return 1
    fi
    local seeder_name
    seeder_name=$(basename "${chroot_dir}")
    echo "${seeder_name}"
}

seeders_lib.seeder_lock_dir() {
    local lock_dir="${MATRIXOS_SEEDER_LOCK_DIR}"
    mkdir -p "${lock_dir}"
    echo "${lock_dir}"
}

seeders_lib.seeder_lock_path() {
    local seeder_name="${1}"
    local lock_dir
    lock_dir="$(seeders_lib.seeder_lock_dir)"
    local lock_file="${lock_dir}/${seeder_name}.lock"
    echo "${lock_file}"
}

seeders_lib.execute_with_seeder_lock() {
    local func="${1}"
    local seeder_name="${2}"
    shift 2

    local lock_path
    lock_path=$(seeders_lib.seeder_lock_path "${seeder_name}")
    echo "Acquiring seeder ${seeder_name} lock via ${lock_path} ... (remove lock and re-run to force)"

    local lock_fd=
    # Do not use a subshell otherwise the global cleanup variables used in trap will not
    # be filled properly. Like: ${MOUNTS} in seeder.
    exec {lock_fd}>"${lock_path}"

    if ! flock -x --timeout "${MATRIXOS_SEEDER_LOCK_WAIT_SECS}" "${lock_fd}"; then
        echo "Timed out waiting for lock ${lock_path}" >&2
        exec {lock_fd}>&-
        return 1
    fi

    echo "Lock for seeder ${seeder_name}, ${lock_path} on FD ${lock_fd} acquired!"

    # We do NOT use a trap. We rely on standard flow control.
    # If "${func}" crashes (set -e), the script dies and OS closes the FD.
    # If "${func}" returns (success or fail), we capture it.
    "${func}" "${@}"
    local ret=${?}

    # Release the lock.
    exec {lock_fd}>&-
    return ${ret}
}