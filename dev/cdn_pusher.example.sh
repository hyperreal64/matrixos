#!/bin/bash
set -e

if [ -e /etc/profile ]; then
    source /etc/profile
fi
set -eu

# Local directory paths.
MATRIXOS_DEV_DIR=/matrixos
source "${MATRIXOS_DEV_DIR}"/headers/env.include.sh

LOCAL_OSTREE_REPO="${MATRIXOS_DEV_DIR}/ostree/repo"
LOCAL_IMAGES_DIR="${MATRIXOS_DEV_DIR}/out/images"
INDEX_FILE="${LOCAL_IMAGES_DIR}/index.html"
LATEST_FILE="${LOCAL_IMAGES_DIR}/LATEST"

prepare_latest_file() {
    echo "generating LATEST file..."
    # Find the latest date (YYYYMMDD) from .img.xz files
    local latest_date=$(find "${LOCAL_IMAGES_DIR}" -maxdepth 1 -type f -name "*.img.xz" -printf "%f\n" | \
        grep -oE '[0-9]{8}' | sort -rn | head -n 1)

    if [ -n "${latest_date}" ]; then
        find "${LOCAL_IMAGES_DIR}" -maxdepth 1 -type f -name "*${latest_date}*.img.xz" -printf "%f\n" | sort > "${LATEST_FILE}"
    else
        : > "${LATEST_FILE}"
    fi
    echo "LATEST file generated."
}

prepare_index_html() {
    echo "generating index.html..."
    # Start HTML Header (Dark Mode style)
    cat > "${INDEX_FILE}" <<EOF
<!DOCTYPE html>
<html>
<head>
    <title>MatrixOS Images</title>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        body { font-family: monospace; background: #1a1a1a; color: #ddd; padding: 20px; }
        h1 { color: #00ff00; border-bottom: 1px solid #444; padding-bottom: 10px; }
        a { text-decoration: none; color: #4da6ff; display: block; padding: 5px 0; }
        a:hover { color: #fff; text-decoration: underline; }
        .size { color: #888; float: right; }
        .item { border-bottom: 1px solid #333; padding: 8px 0; }
    </style>
</head>
<body>
    <h1>Index of /</h1>
    <div class="list">
EOF

    # Loop through files and add them to the list
    # We use 'stat' to get file size in human readable format
    find "${LOCAL_IMAGES_DIR}" -maxdepth 1 -type f ! -name "index.html" | sort | while read -r filepath; do
        local filename=$(basename "${filepath}")
        local filesize=$(ls -lh "${filepath}" | awk '{print $5}')

        echo "        <div class='item'>" >> "${INDEX_FILE}"
        echo "            <a href=\"${filename}\">${filename} <span class='size'>${filesize}</span></a>" >> "${INDEX_FILE}"
        echo "        </div>" >> "${INDEX_FILE}"
    done

    # Close HTML
    cat >> "${INDEX_FILE}" <<EOF
    </div>
    <p style="color: #666; margin-top: 20px;">Generated at $(date)</p>
</body>
</html>
EOF

    echo "index.html generated."
}

push_cloudflare_images() {
    (
        echo "Pushing images at ${LOCAL_IMAGES_DIR} to Cloudflare R2 ..."
        R2_ACCOUNT_ID=__INSERT_R2_ACCOUNT_ID__
        R2_ACCESS_KEY_ID=__INSERT_R2_ACCESS_KEY_ID__
        R2_SECRET_ACCESS_KEY=__INSERT_R2_SECRET_ACCESS_KEY__
        R2_BUCKET_NAME=__INSERT_R2_BUCKET_NAME__
        export RCLONE_CONFIG_R2_TYPE="s3"
        export RCLONE_CONFIG_R2_PROVIDER="Cloudflare"
        export RCLONE_CONFIG_R2_ACCESS_KEY_ID="${R2_ACCESS_KEY_ID}"
        export RCLONE_CONFIG_R2_SECRET_ACCESS_KEY="${R2_SECRET_ACCESS_KEY}"
        export RCLONE_CONFIG_R2_ENDPOINT="https://${R2_ACCOUNT_ID}.r2.cloudflarestorage.com"
        export RCLONE_CONFIG_R2_ACL="private"
        rclone sync "${LOCAL_IMAGES_DIR}" "r2:${R2_BUCKET_NAME}" \
            --transfers 8 \
            --check-first \
            --fast-list \
            --quiet
        echo "Push from ${LOCAL_IMAGES_DIR} to Cloudflare R2 ${R2_BUCKET_NAME} DONE!"
    )
}

_push_cloudflare_ostree_objects() {
    local obj=
    for obj in objects deltas; do
        local src="${LOCAL_OSTREE_REPO}/${obj}"
        if [ ! -d "${src}" ]; then
            mkdir -p "${src}"
        fi
        rclone sync "${src}" "r2:${R2_BUCKET_NAME}/${obj}" \
            --delete-during \
            --size-only \
            --transfers 128 \
            --checkers 128 \
            --immutable \
            --fast-list \
            --quiet
    done
}

_push_cloudflare_ostree_metadata() {
    rclone sync "${LOCAL_OSTREE_REPO}" "r2:${R2_BUCKET_NAME}" \
        --exclude "objects/**" \
        --exclude "deltas/**" \
        --exclude "summary" \
        --exclude "summary.sig" \
        --exclude ".lock" \
        --exclude "tmp/**" \
        --fast-list \
        --delete-during \
        --quiet
}

push_cloudflare_ostree() {
    (
        echo "Pushing ostree repo at ${LOCAL_OSTREE_REPO} to Cloudflare R2 ..."
        R2_ACCOUNT_ID=__INSERT_R2_ACCOUNT_ID__
        R2_ACCESS_KEY_ID=__INSERT_R2_ACCESS_KEY_ID__
        R2_SECRET_ACCESS_KEY=__INSERT_R2_SECRET_ACCESS_KEY__
        R2_BUCKET_NAME=__INSERT_R2_BUCKET_NAME__
        export RCLONE_CONFIG_R2_TYPE="s3"
        export RCLONE_CONFIG_R2_PROVIDER="Cloudflare"
        export RCLONE_CONFIG_R2_ACCESS_KEY_ID="${R2_ACCESS_KEY_ID}"
        export RCLONE_CONFIG_R2_SECRET_ACCESS_KEY="${R2_SECRET_ACCESS_KEY}"
        export RCLONE_CONFIG_R2_ENDPOINT="https://${R2_ACCOUNT_ID}.r2.cloudflarestorage.com"
        export RCLONE_CONFIG_R2_ACL="private"
        # Sync is ok because this repo persists history of objects for 2 months.
        # See matrixos.git config to confirm.
        _push_cloudflare_ostree_objects
        echo "Push from ${LOCAL_OSTREE_REPO}/objects to Cloudflare R2 ${R2_BUCKET_NAME}/objects DONE!"

        echo "Pushing the refs, config, other metadata, excluding summary to ${R2_BUCKET_NAME} Cloudflare R2 ..."
        _push_cloudflare_ostree_metadata
        echo "Push from ${LOCAL_OSTREE_REPO} (refs, config, other metadata) to Cloudflare R2 ${R2_BUCKET_NAME} DONE!"

        echo "Atomic switch, uploading the summary{,.sig} file to ${R2_BUCKET_NAME} Cloudflare R2 ..."
        rclone copy "${LOCAL_OSTREE_REPO}/summary" "r2:${R2_BUCKET_NAME}" \
            --quiet \
            --s3-no-check-bucket
        if [[ -f "${LOCAL_OSTREE_REPO}/summary.sig" ]]; then
            rclone copy "${LOCAL_OSTREE_REPO}/summary.sig" "r2:${R2_BUCKET_NAME}" \
            --quiet \
            --s3-no-check-bucket
        fi
        echo "Push from ${LOCAL_OSTREE_REPO} summary files to Cloudflare R2 ${R2_BUCKET_NAME} DONE!"
    )
}


main() {
    local built_releases="${MATRIXOS_BUILT_RELEASES:-}"
    if [ -n "${built_releases}" ]; then
        echo "Pushing built releases: ${built_releases} ..."
        push_cloudflare_ostree
    else
        echo "WARNING: No new releases were built. Not pushing!" >&2
    fi

    local built_images="${MATRIXOS_BUILT_IMAGES:-0}"
    if [ "${built_images}" = "1" ]; then
        echo "Pushing built images: ${built_images} ..."
        prepare_latest_file
        prepare_index_html
        push_cloudflare_images
    else
        echo "WARNING: No new images were built. Not pushing!" >&2
    fi
}

main "${@}"