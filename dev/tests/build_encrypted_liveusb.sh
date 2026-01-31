#!/bin/bash
# build_liveusb.sh - dev script to build a liveusb image with specific settings
#                    and encryption enabled.
set -eu

export MATRIXOS_DEV_DIR=$(realpath $(dirname "${0}")/../../)
export MATRIXOS_LIVEOS_ENCRYPTION=1
export MATRIXOS_LIVEOS_ENCRYPTION_KEY=matrix
exec "${MATRIXOS_DEV_DIR}/image/image_main.sh" --ref=matrixos/amd64/dev/gnome "${@}"
