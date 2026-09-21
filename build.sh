#!/usr/bin/env bash
set -euo pipefail
# Retain the original Linux/amd64 default. The new command defaults to this host.
export GOOS="${GOOS:-linux}" GOARCH="${GOARCH:-amd64}"
exec "$(dirname "$0")/scripts/build-backend" "$@"
