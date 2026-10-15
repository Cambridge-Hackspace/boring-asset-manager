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
# The version the binaries report: a release's tag (the release workflow sets
# VERSION), or what git describe says of this checkout.
VERSION="${VERSION:-$(cd "$ROOT" && git describe --tags --always --dirty 2>/dev/null || echo dev)}"
# go build asks git which commit it is building, and git refuses a checkout
# owned by another user: the mounted one is, when the container runs as root
# on a CI runner. It is the checkout being built, so trust it.
TRUST_SRC="git config --global --add safe.directory /src"

"$ROOT/tools/web/build.sh"

mkdir -p "$ROOT/bin"
for target in $TARGETS; do
  os="${target%/*}"
  arch="${target#*/}"
  echo "building bin/bam-$os-$arch ($VERSION)"
  # $VERSION, $GOOS and $GOARCH expand inside the container.
  # shellcheck disable=SC2016
  MSYS_NO_PATHCONV=1 "$CONTAINER" run --rm \
    -v "$ROOT:/src" \
    -v bam-gocache:/root/.cache/go-build \
    -v bam-gomod:/go/pkg/mod \
    -w /src \
    -e CGO_ENABLED=0 -e GOOS="$os" -e GOARCH="$arch" -e VERSION="$VERSION" \
    "$GO_IMAGE" \
    sh -c "$TRUST_SRC"' && go build -trimpath -ldflags "-s -w -X github.com/Cambridge-Hackspace/boring-asset-manager/internal/api.Version=$VERSION" -o "bin/bam-$GOOS-$GOARCH" ./cmd/bam'
done

# Checksums of every binary in bin/, which the updater verifies.
MSYS_NO_PATHCONV=1 "$CONTAINER" run --rm -v "$ROOT:/src" -w /src/bin "$GO_IMAGE" \
  sh -c 'sha256sum bam-* > SHA256SUMS && cat SHA256SUMS'
