#!/bin/bash
set -e

if [ -e /etc/profile ]; then
    source /etc/profile
fi

set -eu

cd "$(dirname "$0")"

cd ../.. # the root

mkdir -p dev/janitor/bin
echo "Building janitor..."
go build -o dev/janitor/bin/janitor ./dev/janitor

echo "Running janitor..."
exec dev/janitor/bin/janitor "${@}"
