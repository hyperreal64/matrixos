#!/bin/bash
# Temporary library for migrating codebase to Golang.
set -eu

# The "special var".
DEFAULT_MATRIXOS_DEV_DIR=/matrixos
MATRIXOS_DEV_DIR=${MATRIXOS_DEV_DIR:-"${DEFAULT_MATRIXOS_DEV_DIR}"}
source "${MATRIXOS_DEV_DIR}"/lib/ini_lib.sh

__cfg="${MATRIXOS_DEV_DIR}/conf/matrixos.conf"
if [ ! -f "${__cfg}" ]; then
    echo "Configuration file ${__cfg} not found. Unable to continue." >&2
    exit 1
fi

__env_lib_path="$(realpath "${BASH_SOURCE[0]}")"
__cur_dev_dir="$(realpath "$(dirname "${__env_lib_path}")/../")"

_is_absolute_path() {
    local path="${1}"
    if [ "${path}" != "${path#/}" ]; then
        return 0
    else
        return 1
    fi
}

env_lib.get_root() {
    local default_value="${1:-}"

    local value=
    value=$(ini_lib.get "${__cfg}" "matrixOS" "Root")
    if [ -z "${value}" ]; then
        value="${default_value}"
    fi
    if [ -z "${value}" ]; then
        echo "matrixOS.Root is not set in the configuration file and no default value provided." >&2
        return 1
    fi

    # Support for relative paths.
    if _is_absolute_path "${value}"; then
        # If value is an absolute path, return it as-is.
        echo "${value}"
    else
        # If value is a relative path, make it absolute.
        echo "$(realpath "${__cur_dev_dir}/${value}")"
    fi
}

env_lib.get_root_var() {
    local root="${1}"
    if [ -z "${root}" ]; then
        echo "Root directory is required." >&2
        return 1
    fi

    local section="${2}"
    if [ -z "${section}" ]; then
        echo "Section name is required." >&2
        return 1
    fi
    local var_name="${3}"
    if [ -z "${var_name}" ]; then
        echo "Variable name is required." >&2
        return 1
    fi
    local default_value="${4:-}"

    local value
    value=$(ini_lib.get "${__cfg}" "${section}" "${var_name}")
    if [ -z "${value}" ]; then
        value="${default_value}"
    fi

    # Support for relative paths.
    if ! _is_absolute_path "${value}"; then
        # If value is a relative path, make it absolute.
        value="$(realpath "${root}")/${value}"
    fi
    echo "${value}"
}

env_lib.get_simple_var() {
    local section="${1}"
    if [ -z "${section}" ]; then
        echo "Section name is required." >&2
        return 1
    fi
    local var_name="${2}"
    if [ -z "${var_name}" ]; then
        echo "Variable name is required." >&2
        return 1
    fi
    local default_value="${3:-}"

    local value
    value=$(ini_lib.get "${__cfg}" "${section}" "${var_name}")
    if [ -z "${value}" ]; then
        value="${default_value}"
    fi

    echo "${value}"
}

env_lib.get_bool_var() {
    local section="${1}"
    if [ -z "${section}" ]; then
        echo "Section name is required." >&2
        return 1
    fi
    local var_name="${2}"
    if [ -z "${var_name}" ]; then
        echo "Variable name is required." >&2
        return 1
    fi
    local default_value="${3:-}"

    local value
    value=$(ini_lib.get "${__cfg}" "${section}" "${var_name}")
    if [ -z "${value}" ]; then
        value="${default_value}"
    fi

    if [[ "${value}" =~ ^[Yy][Ee][Ss]$ || "${value}" =~ ^[Tt][Rr][Uu][Ee]$ || "${value}" == "1" ]]; then
        echo "1"
    fi
}
