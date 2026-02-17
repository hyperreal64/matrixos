#!/bin/bash
set -e

if [ -e /etc/profile ]; then
    source /etc/profile
fi
set -eu

if [ -z "${MATRIXOS_DEV_DIR:-}" ]; then
    echo "MATRIXOS_DEV_DIR required." >&2
    exit 1
fi

if [ -z "${MATRIXOS_LOGS_DIR:-}" ]; then
    echo "MATRIXOS_LOGS_DIR required." >&2
    exit 1
fi

if [ -z "${IMAGE_PATH:-}" ]; then
    echo "IMAGE_PATH required." >&2
    exit 1
fi

if [ -z "${REF:-}" ]; then
    echo "REF required." >&2
    exit 1
fi

LOG_DATE=$(date +"%Y%m%d_%H%M%S")
LOG_FILE="${MATRIXOS_LOGS_DIR}/${REF//\//_}_${LOG_DATE}.log"
echo "Logging VM Test for ${REF} to ${LOG_FILE} ..."

"${MATRIXOS_DEV_DIR}/vector/tests/smoke.sh" "${IMAGE_PATH}" > "${LOG_FILE}"