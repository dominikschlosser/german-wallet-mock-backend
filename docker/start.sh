#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
UPSTREAM_REVISION=$(git -C upstream/backend rev-parse HEAD)
export UPSTREAM_REVISION
exec docker compose up -d --build --wait "$@"
