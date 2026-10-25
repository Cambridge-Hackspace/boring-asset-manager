#!/bin/sh
# Runs the end-to-end and accessibility battery (web/e2e) against a real
# `bam serve` on a fresh SQLite database, in the pinned Playwright container.
# Builds the web interface and a Linux binary first. Needs only podman (or
# docker, with CONTAINER=docker).
#
#   tools/web/e2e.sh                 run every spec
#   tools/web/e2e.sh editor a11y     run the specs whose names match
set -eu

GO_IMAGE="docker.io/library/golang@sha256:5cf287a799e6b94384bad13d16b14904c531f51ba65792237e122ce42b392f61"
# playwright:v1.64.0-noble, by digest alone (reaper refuses a tag beside a digest)
PLAYWRIGHT_IMAGE="mcr.microsoft.com/playwright@sha256:bc72a8df40831ded821e3cbc79c318c21b44d6e51dff9fd89ace7a8249c5b462"
CONTAINER="${CONTAINER:-podman}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
# Where node_modules and the npm cache live: named volumes by default, or
# directories (reaper points them at its caches).
NODE_MODULES="${BAM_WEB_NODE_MODULES:-bam-web-node-modules}"
NPM_CACHE="${BAM_NPM_CACHE:-bam-npm-cache}"
# Go's build and module caches, likewise.
GO_CACHE="${BAM_GO_CACHE:-bam-gocache}"
GO_MOD="${BAM_GO_MOD:-bam-gomod}"

"$ROOT/tools/web/build.sh"
# The icon compiler, which the binary embeds, checked against wasm.lock.
"$ROOT/tools/isolex/fetch.sh"

# go build asks git which commit it is building, and git refuses a checkout
# owned by another user (as on a CI runner or reaper guest): trust the mount.
mkdir -p "$ROOT/web/.e2e"
MSYS_NO_PATHCONV=1 "$CONTAINER" run --rm \
  -v "$ROOT:/src" -w /src \
  -v "$GO_CACHE:/root/.cache/go-build" -v "$GO_MOD:/go/pkg/mod" \
  -e CGO_ENABLED=0 \
  "$GO_IMAGE" sh -c "git config --global --add safe.directory /src && go build -o web/.e2e/bam ./cmd/bam"

# The script below expands its variables inside the container.
# shellcheck disable=SC2016
MSYS_NO_PATHCONV=1 "$CONTAINER" run --rm \
  -v "$ROOT:/src" \
  -v "$NODE_MODULES:/src/web/node_modules" \
  -v "$NPM_CACHE:/root/.npm" \
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
    # Notifications must never leave the container: point outgoing HTTPS at
    # a closed port, so deliveries fail as unreachable and are retried.
    HTTPS_PROXY=http://127.0.0.1:9 "$bin" serve --config c.toml >"$state/server.log" 2>&1 &
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
