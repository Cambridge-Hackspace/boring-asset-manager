#!/bin/sh
# Builds release binaries, with the web interface embedded, into bin/:
# bin/bam-<os>-<arch>. Runs in pinned containers, so only podman (or docker,
# with CONTAINER=docker) is needed.
#
#   tools/release/build.sh                        linux/amd64, linux/arm64 and freebsd/amd64
#   tools/release/build.sh linux/amd64 freebsd/arm64   just these
set -eu

GO_IMAGE="docker.io/library/golang@sha256:167053a2bb901972bf2c1611f8f52c44d5fe7e762e5cab213708d82c421614db"
CONTAINER="${CONTAINER:-podman}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TARGETS="${*:-linux/amd64 linux/arm64 freebsd/amd64}"

"$ROOT/tools/web/build.sh"

mkdir -p "$ROOT/bin"
for target in $TARGETS; do
  os="${target%/*}"
  arch="${target#*/}"
  echo "building bin/bam-$os-$arch"
  MSYS_NO_PATHCONV=1 "$CONTAINER" run --rm \
    -v "$ROOT:/src" \
    -v bam-gocache:/root/.cache/go-build \
    -v bam-gomod:/go/pkg/mod \
    -w /src \
    -e CGO_ENABLED=0 -e GOOS="$os" -e GOARCH="$arch" \
    "$GO_IMAGE" \
    go build -trimpath -ldflags "-s -w" -o "bin/bam-$os-$arch" ./cmd/bam
done
