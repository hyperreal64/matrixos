#!/bin/bash
set -eu

TARGET="${1}"

if [ -z "${TARGET}" ]; then
    echo "Usage: docker run ... matrixos/installer /dev/sdX"
    echo "⚠️  WARNING: This will wipe the target disk!"
    exit 1
fi

if [ ! -b "${TARGET}" ]; then
    echo "Error: ${TARGET} is not a block device."
    exit 1
fi

echo "Flashing matrixOS to ${TARGET}..."
xz -d -c /opt/matrixos.img.xz | dd of="${TARGET}" bs=4M status=progress conv=fsync