#!/bin/bash
set -eu

dev_dir=$(realpath $(dirname "${0}"))
export MATRIXOS_DEV_DIR="$(realpath ${dev_dir}/../../)"

echo "MATRIXOS_DEV_DIR=${MATRIXOS_DEV_DIR}"
imager_exec="${dev_dir}/../../image/image.releases"

exec "${imager_exec}" "${@}"
