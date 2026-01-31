#!/bin/bash
# build_liveusb.sh - dev script to build a liveusb image with specific settings.
set -eu

export MATRIXOS_DEV_DIR=$(realpath $(dirname "${0}")/../../)
export MATRIXOS_LIVEOS_ENCRYPTION=
exec "${MATRIXOS_DEV_DIR}/image/image_main.sh" --ref=matrixos/amd64/dev/gnome "${@}"
