#!/bin/sh
# The reaper battery (README §8.1): the Go suites, then the web interface's
# checks and its Playwright battery, each in its pinned container. reaper
# runs this on the guest itself (exec = "host"), because a containerized step
# cannot start containers of its own; the images it uses are listed under
# run.images in .reaper.toml so reaper pulls them first.
#
# It needs REAPER_STATE and the caches declared in .reaper.toml
# (REAPER_CACHE_GOCACHE, _GOPATH, _NPM and _NODE_MODULES). Run by hand with
# those pointing at directories of your own, it does the same.
set -eu

GO_IMAGE="docker.io/library/golang@sha256:167053a2bb901972bf2c1611f8f52c44d5fe7e762e5cab213708d82c421614db"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
CONTAINER="${CONTAINER:-podman}"
export CONTAINER MSYS_NO_PATHCONV=1

step() { printf '\n=== %s\n' "$*"; }

step "Go suites (-race), on SQLite in REAPER_STATE"
mkdir -p "$REAPER_STATE/uploads"
"$CONTAINER" run --rm \
  -v "$ROOT:/src" -w /src \
  -v "$REAPER_CACHE_GOCACHE:/root/.cache/go-build" \
  -v "$REAPER_CACHE_GOPATH:/go" \
  -v "$REAPER_STATE:/state" \
  -e BAM_DB_PATH=/state/bam_test.db \
  -e BAM_STORAGE_PATH=/state/uploads \
  "$GO_IMAGE" \
  go test -race ./...

# The web scripts keep node_modules, the npm cache and Go's caches in
# reaper's caches rather than in named volumes on the guest's small root disk.
BAM_WEB_NODE_MODULES="$REAPER_CACHE_NODE_MODULES"
BAM_NPM_CACHE="$REAPER_CACHE_NPM"
BAM_GO_CACHE="$REAPER_CACHE_GOCACHE"
BAM_GO_MOD="$REAPER_CACHE_GOPATH/pkg/mod"
export BAM_WEB_NODE_MODULES BAM_NPM_CACHE BAM_GO_CACHE BAM_GO_MOD

step "web interface: svelte-check and Vitest"
"$ROOT/tools/web/build.sh" check

step "web interface: Playwright and axe"
"$ROOT/tools/web/e2e.sh"

step "all passed"
