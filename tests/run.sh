#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
UPSTREAM_REVISION=$(git -C upstream/backend rev-parse HEAD)
export UPSTREAM_REVISION
WALLET_TEST_PROJECT="wallet-test-$(date +%s)-$$-$RANDOM"
export WALLET_TEST_PROJECT
export MOCK_BIND_ADDRESS=127.0.0.1

for attempt in {1..20}; do
    port=$((32768 + RANDOM % 28000))
    if ! (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then
        break
    fi
    if [[ "$attempt" == 20 ]]; then
        echo "Could not find a free test port" >&2
        exit 1
    fi
done
export MOCK_PORT="$port"
export MOCK_PUBLIC_URL="http://localhost:$MOCK_PORT"
export WALLET_TEST_URL="$MOCK_PUBLIC_URL"
compose=(docker compose -p "$WALLET_TEST_PROJECT")

cleanup() {
    "${compose[@]}" stop || true
    echo "Test stack stopped. Diagnostic state retained in Compose project $WALLET_TEST_PROJECT"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

COMPOSE_PROJECT_NAME="$WALLET_TEST_PROJECT" bash docker/start.sh --wait-timeout 120
go test -race -v ./tests -count=1
