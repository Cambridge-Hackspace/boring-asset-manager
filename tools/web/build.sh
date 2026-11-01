#!/bin/sh
# Builds the web interface into internal/webui/static/app, where `go build`
# embeds it. The template editor's live renderer (cmd/bamlive) is built
# first, to WebAssembly with TinyGo, into web/src/lib/live/generated. Runs
# in pinned containers, so only podman (or docker, with CONTAINER=docker) is
# needed.
#
#   tools/web/build.sh          build the interface
#   tools/web/build.sh check    type-check and run the unit tests instead
set -eu

# node:22-alpine, by digest alone (reaper refuses a tag beside a digest)
NODE_IMAGE="docker.io/library/node@sha256:2c752226d477b4a886378baa95b9af252be59301b725fdb0b7e15208131505a8"
# tinygo/tinygo:0.43.0 (Go 1.27.2), by digest alone
TINYGO_IMAGE="docker.io/tinygo/tinygo@sha256:3470986dbce0b43cc5c590d46b8770f656528bc3e9f29267214d464c0fbbe9c5"
CONTAINER="${CONTAINER:-podman}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
# Where node_modules and the npm cache live: named volumes by default, or
# directories (reaper points them at its caches).
NODE_MODULES="${BAM_WEB_NODE_MODULES:-bam-web-node-modules}"
NPM_CACHE="${BAM_NPM_CACHE:-bam-npm-cache}"
# Go's module cache, and TinyGo's own (its build of the standard library).
GO_MOD="${BAM_GO_MOD:-bam-gomod}"
TINYGO_CACHE="${BAM_TINYGO_CACHE:-bam-tinygo-cache}"

case "${1:-build}" in
  build) STEPS="npm run build" ;;
  check) STEPS="npm run check && npm test" ;;
  *) echo "usage: $0 [build|check]" >&2; exit 2 ;;
esac

# The live renderer: the label renderer the server prints with, compiled to
# WebAssembly (docs/labels.md). TinyGo's build is a third the size of Go's and
# draws every golden label dot for dot as the server does (web/src/lib/live
# tests that); its default stack is too small for compiling the renderer's
# regular expressions, hence -stack-size. It is built without the git
# stamp, which it has no use for: where the checkout is not root's, as on
# GitHub's runners under docker, git refuses to read it and the build fails.
mkdir -p "$ROOT/web/src/lib/live/generated"
MSYS_NO_PATHCONV=1 "$CONTAINER" run --rm --user root -e GOFLAGS=-buildvcs=false   -v "$ROOT:/src" -w /src   -v "$GO_MOD:/root/go/pkg/mod"   -v "$TINYGO_CACHE:/root/.cache"   "$TINYGO_IMAGE"   sh -c 'tinygo build -target wasm -opt z -no-debug -stack-size=1MB -o web/src/lib/live/generated/bamlive.wasm ./cmd/bamlive &&
    cp "$(tinygo env TINYGOROOT)/targets/wasm_exec.js" web/src/lib/live/generated/wasm_exec.js'

# node_modules lives in a volume: it holds Linux binaries, and bind mounts
# of thousands of small files are slow on Windows and macOS.
MSYS_NO_PATHCONV=1 "$CONTAINER" run --rm \
  -v "$ROOT:/src" \
  -v "$NODE_MODULES:/src/web/node_modules" \
  -v "$NPM_CACHE:/root/.npm" \
  -w /src/web \
  "$NODE_IMAGE" \
  sh -c "npm ci --no-audit --no-fund && $STEPS"
