#!/bin/sh
# Fetches the isolex compiler, built to WebAssembly, into
# internal/isolex/wasm/isolex.wasm, where go build embeds it (README §1,
# "Icon compiler"). internal/isolex/wasm.lock pins the release and its
# SHA-256: a download that does not match is refused and nothing is
# written, so a compromised registry or mirror cannot slip in another
# compiler. Runs in the pinned Go image, so only podman (or docker, with
# CONTAINER=docker) is needed. A module already in place that matches is
# kept.
set -eu

GO_IMAGE="docker.io/library/golang@sha256:5cf287a799e6b94384bad13d16b14904c531f51ba65792237e122ce42b392f61"
CONTAINER="${CONTAINER:-podman}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

MSYS_NO_PATHCONV=1 "$CONTAINER" run --rm -v "$ROOT:/src" -w /src/internal/isolex "$GO_IMAGE" sh -eu -c '
  url=$(sed -n "s/^url //p" wasm.lock)
  sum=$(sed -n "s/^sha256 //p" wasm.lock)
  version=$(sed -n "s/^version //p" wasm.lock)
  [ -n "$url" ] && [ ${#sum} = 64 ] || { echo "wasm.lock needs a url and a sha256" >&2; exit 1; }
  if [ -f wasm/isolex.wasm ] && echo "$sum  wasm/isolex.wasm" | sha256sum -c --status; then
    echo "isolex $version: already in place"
    exit 0
  fi
  tmp=$(mktemp)
  trap "rm -f \"$tmp\"" EXIT
  curl -fsSL --proto =https --max-time 300 -o "$tmp" "$url"
  if ! echo "$sum  $tmp" | sha256sum -c --status; then
    echo "REFUSED: $url has SHA-256 $(sha256sum "$tmp" | cut -d" " -f1), but wasm.lock pins $sum" >&2
    exit 1
  fi
  cp "$tmp" wasm/isolex.wasm
  echo "isolex $version: fetched, SHA-256 $sum"
'
