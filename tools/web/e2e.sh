#!/bin/sh
# Runs the end-to-end and accessibility battery (web/e2e) against a real
# `bam serve` on a fresh SQLite database, in the pinned Playwright container.
# Builds the web interface and a Linux binary first. Needs only podman (or
# docker, with CONTAINER=docker).
#
#   tools/web/e2e.sh                 run every spec
#   tools/web/e2e.sh editor a11y     run the specs whose names match
set -eu

GO_IMAGE="docker.io/library/golang@sha256:167053a2bb901972bf2c1611f8f52c44d5fe7e762e5cab213708d82c421614db"
PLAYWRIGHT_IMAGE="mcr.microsoft.com/playwright:v1.64.0-noble@sha256:bc72a8df40831ded821e3cbc79c318c21b44d6e51dff9fd89ace7a8249c5b462"
CONTAINER="${CONTAINER:-podman}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

"$ROOT/tools/web/build.sh"

mkdir -p "$ROOT/web/.e2e"
MSYS_NO_PATHCONV=1 "$CONTAINER" run --rm \
  -v "$ROOT:/src" -w /src \
  -v bam-gocache:/root/.cache -v bam-gomod:/go/pkg \
  -e CGO_ENABLED=0 \
  "$GO_IMAGE" go build -o web/.e2e/bam ./cmd/bam

MSYS_NO_PATHCONV=1 "$CONTAINER" run --rm \
  -v "$ROOT:/src" \
  -v bam-web-node-modules:/src/web/node_modules \
  -v bam-npm-cache:/root/.npm \
  -w /src/web \
  -e E2E_EMAIL=ada@hackspace.org -e E2E_PASSWORD='Pass1234!' \
  "$PLAYWRIGHT_IMAGE" \
  sh -c '
    set -e
    npm ci --no-audit --no-fund >/dev/null
    state=$(mktemp -d)
    bin=$state/bam
    cp .e2e/bam "$bin" && chmod +x "$bin"
    cd "$state"
    "$bin" init --config c.toml >/dev/null
    sed -i "s#data/bam.db#$state/bam.db#; s#./data/uploads#$state/uploads#" c.toml
    "$bin" migrate --config c.toml >/dev/null
    "$bin" admin create --config c.toml --email "$E2E_EMAIL" --password "$E2E_PASSWORD" >/dev/null
    "$bin" serve --config c.toml >"$state/server.log" 2>&1 &
    for i in $(seq 1 50); do
      wget -q -O /dev/null http://127.0.0.1:8080/healthz && break
      sleep 0.2
    done
    cd /src/web
    status=0
    npx playwright test "$@" || status=$?
    if [ $status -ne 0 ]; then echo "--- server log ---"; tail -50 "$state/server.log"; fi
    exit $status
  ' sh "$@"
