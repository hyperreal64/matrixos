#!/bin/bash
set -eu

./_test_client_imager.sh -o origin:matrixos/amd64/dev/gnome "${@}"
