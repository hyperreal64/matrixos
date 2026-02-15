#!/bin/bash
set -eu

BUILD_FILE="/usr/share/matrixos/seeder/metadata/build"

_is_seed() {
    local seed_prefix="${1}"
    local seed_name=$(cat "${BUILD_FILE}" | grep SEED_NAME= 2>/dev/null || echo "")
    [[ "${seed_name}" == *"${seed_prefix}"* ]] && return 0
    return 1
}

_is_bedrock() {
    _is_seed "bedrock"
}

_is_server() {
    _is_seed "server"
}

_is_gnome() {
    _is_seed "gnome"
}

_is_cosmic() {
    _is_seed "cosmic"
}

test.etc_resolv_conf() {
    local resolv_conf="/etc/resolv.conf"

    local i=
    local found=
    for i in {1..10}; do
        if [[ -f "${resolv_conf}" ]] && ( cat "${resolv_conf}" > /dev/null ); then
            cat "${resolv_conf}"
            echo "resolv.conf found: ${resolv_conf}"
            found=1
            break
        fi
        sleep 2
    done

    if [ -z "${found}" ]; then
        echo "resolv.conf not found or invalid: ${resolv_conf}"
        return 1
    fi

    grep -q "nameserver" "${resolv_conf}" || {
        echo "No nameserver entry found in ${resolv_conf}"
        return 1
    }
}

test.build_file() {
    if [[ ! -f "${BUILD_FILE}" ]]; then
        echo "Build file not found: ${BUILD_FILE}"
        return 1
    fi
}

test.os_release() {
    local os_release="/etc/os-release"
    if [[ ! -f "${os_release}" ]]; then
        echo "os-release file not found: ${os_release}"
        return 1
    fi
    if ! grep -q "ID=" "${os_release}"; then
        echo "ID field not found in ${os_release}"
        return 1
    fi
    local matrixos_id="matrixos"
    if ! grep -q "${matrixos_id}" "${os_release}"; then
        echo "ID does not contain ${matrixos_id} in ${os_release}"
        return 1
    fi
}


main() {

    local tests=(
        test.etc_resolv_conf
        test.build_file
        test.os_release
    )

    local exit_st=0
    for test in "${tests[@]}"; do
        if ! "${test}"; then
            echo "Test failed: ${test}"
            exit_st=1
        else
            echo "Test passed: ${test}"
        fi
    done
    return "${exit_st}"
}

main "${@}"