#!/bin/sh
# Builds one release archive: scripts/build-release.sh VERSION OUTDIR
# On macOS it builds natively for the host architecture. For Linux it builds a
# fully static binary inside the pinned Go Alpine image, so the result runs on any
# distribution regardless of its C library. BUILD_OS=Linux forces the Linux build
# on another host with Docker, for local testing.
set -eu
version=$1
out=$2
image="golang:1.26.10-alpine@sha256:a2012b4a1d1f9f7e9f44e0d39b805a901e856b1dd80b4e6ee4338db547050308"
ldflags="-s -w -X github.com/rburns629/urfluency/internal/cli.Version=$version"
mkdir -p "$out"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

case "${BUILD_OS:-$(uname -s)}" in
Darwin)
  goos=darwin
  goarch=$(go env GOARCH)
  # Run on macOS 13 and later, the oldest the Go toolchain targets, not only the
  # build runner's release.
  MACOSX_DEPLOYMENT_TARGET=13.0 CGO_ENABLED=1 go build -trimpath -ldflags "$ldflags" -o "$work/urfluency" ./cmd/urfluency
  ;;
Linux)
  goos=linux
  goarch=$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')
  docker run --rm -v "$PWD":/src -v "$work":/out -w /src -e CGO_ENABLED=1 -e GOFLAGS=-buildvcs=false "$image" \
    sh -c "apk add --no-cache build-base >/dev/null && go build -trimpath -ldflags '$ldflags -linkmode external -extldflags -static' -o /out/urfluency ./cmd/urfluency"
  ;;
*)
  echo "unsupported build host: $(uname -s)" >&2
  exit 1
  ;;
esac

cp LICENSE README.md "$work/"
archive="urfluency_${version}_${goos}_${goarch}.tar.gz"
tar -C "$work" -czf "$out/$archive" urfluency LICENSE README.md
echo "$out/$archive"
