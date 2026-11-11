#!/usr/bin/env bash
# Build a Linux x86_64 server bundle: dist/kakao-beeper-server.tar.gz
# The bridge needs cgo (sqlite), so it is built in a Debian bookworm container
# (glibc 2.36) to run on Ubuntu 24.04 and other current distros.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="$here/dist/server"
rm -rf "$out" && mkdir -p "$out/sidecar"

echo "== Building bridge (golang:1.27.3-bookworm)"
cache="${XDG_CACHE_HOME:-$HOME/.cache}/kakao-beeper-build"
mkdir -p "$cache"
docker run --rm -u "$(id -u):$(id -g)" \
    -v "$here:/src:ro" -v "$out:/out" -v "$cache:/cache" \
    -e HOME=/tmp -e GOMODCACHE=/cache/mod -e GOCACHE=/cache/build -e GOFLAGS=-buildvcs=false \
    -w /src golang:1.27.3-bookworm \
    go build -tags goolm -trimpath -o /out/mautrix-kakaotalk ./cmd/mautrix-kakaotalk

echo "== Installing sidecar dependencies"
cp "$here"/sidecar/{sidecar.mjs,package.json,package-lock.json,check-imports.mjs} "$out/sidecar/"
npm ci --prefix "$out/sidecar" --omit=optional --ignore-scripts --no-audit --no-fund
(cd "$out/sidecar" && node check-imports.mjs)

cp "$here"/server/{install.sh,kakao-beeper.service} "$out/"
git -C "$here" describe --always --dirty --abbrev=7 >"$out/COMMIT"
tar -C "$here/dist" -czf "$here/dist/kakao-beeper-server.tar.gz" server
echo "== $(du -h "$here/dist/kakao-beeper-server.tar.gz" | cut -f1) dist/kakao-beeper-server.tar.gz"
