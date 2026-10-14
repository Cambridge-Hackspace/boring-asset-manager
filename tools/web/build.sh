#!/bin/sh
# Builds the web interface into internal/webui/static/app, where `go build`
# embeds it. Runs in a pinned Node container, so only podman (or docker,
# with CONTAINER=docker) is needed.
#
#   tools/web/build.sh          build the interface
#   tools/web/build.sh check    type-check and run the unit tests instead
set -eu

# node:22-alpine, by digest alone (reaper refuses a tag beside a digest)
NODE_IMAGE="docker.io/library/node@sha256:2c752226d477b4a886378baa95b9af252be59301b725fdb0b7e15208131505a8"
CONTAINER="${CONTAINER:-podman}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
# Where node_modules and the npm cache live: named volumes by default, or
# directories (reaper points them at its caches).
NODE_MODULES="${BAM_WEB_NODE_MODULES:-bam-web-node-modules}"
NPM_CACHE="${BAM_NPM_CACHE:-bam-npm-cache}"

case "${1:-build}" in
  build) STEPS="npm run build" ;;
  check) STEPS="npm run check && npm test" ;;
  *) echo "usage: $0 [build|check]" >&2; exit 2 ;;
esac

# node_modules lives in a volume: it holds Linux binaries, and bind mounts
# of thousands of small files are slow on Windows and macOS.
MSYS_NO_PATHCONV=1 "$CONTAINER" run --rm \
  -v "$ROOT:/src" \
  -v "$NODE_MODULES:/src/web/node_modules" \
  -v "$NPM_CACHE:/root/.npm" \
  -w /src/web \
  "$NODE_IMAGE" \
  sh -c "npm ci --no-audit --no-fund && $STEPS"
