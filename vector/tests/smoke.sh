#!/bin/bash
set -eu

# TODO: remove this once moved to the new build server.
echo "SMOKE TEST CURRENTLY DISABLED, PENDING MOVE TO NEW SERVER" >&2
exit 0

IMAGE_PATH="${1}"
shift

cd "$(realpath "$(dirname "${0}")")"
cd ..
./vector dev vm -image "${IMAGE_PATH}" -noaudio -nographic "${@}"
