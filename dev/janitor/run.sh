#!/bin/bash
set -e

if [ -e /etc/profile ]; then
    source /etc/profile
fi

set -eu


cd "$(dirname "$0")"

mkdir -p bin
echo "Building janitor..."
go build -o bin/janitor .

echo "Running janitor..."
exec ./bin/janitor "${@}"
