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

_env_lib_get_config_value() {
    local section="${1}"
    local key="${2}"
    local default_value="${3:-}"

    local value=""

    # Check main config file
    if [ -f "${__cfg}" ]; then
        value=$(ini_lib.get "${__cfg}" "${section}" "${key}")
    fi

    # Check override files in matrixos.conf.d/*.conf
    local conf_d="${__cfg}.d"
    if [ -d "${conf_d}" ]; then
        # Iterate over .conf files in alphabetical order
        local f=
        for f in "${conf_d}"/*.conf; do
            [ -f "${f}" ] || continue
            local val
            val=$(ini_lib.get "${f}" "${section}" "${key}")
            if [ -n "${val}" ]; then
                value="${val}"
            fi
        done
    fi

    if [ -z "${value}" ]; then
        echo "${default_value}"
    else
        echo "${value}"
    fi
}

env_lib.get_root() {
    local default_value="${1:-}"

    local value
    value=$(_env_lib_get_config_value "matrixOS" "Root" "${default_value}")
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
    value=$(_env_lib_get_config_value "${section}" "${var_name}" "${default_value}")

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

    _env_lib_get_config_value "${section}" "${var_name}" "${default_value}"
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
    value=$(_env_lib_get_config_value "${section}" "${var_name}" "${default_value}")

    if [[ "${value}" =~ ^[Yy][Ee][Ss]$ || "${value}" =~ ^[Tt][Rr][Uu][Ee]$ || "${value}" == "1" ]]; then
        echo "1"
    fi
}
