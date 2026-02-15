#!/bin/bash
set -eu

IMAGE_PATH="${1}"
shift

cd "$(realpath "$(dirname "${0}")")"
cd ..
./vector dev vm -image "${IMAGE_PATH}" -interactive "${@}"
