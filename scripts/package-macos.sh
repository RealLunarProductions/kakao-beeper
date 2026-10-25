#!/usr/bin/env bash
# Build the macOS install package: dist/kakao-beeper-macos-<version>.tar.gz
#
#   scripts/package-macos.sh                 source-only package (the Mac builds the bridge itself)
#   KB_BIN_DIR=out scripts/package-macos.sh  also bundle out/mautrix-kakaotalk-darwin-{arm64,amd64}
#
# Packages the committed state (git archive), not the working tree.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$here"
out="${KB_OUT_DIR:-$here/dist}"
version="$(git describe --tags --always 2>/dev/null || git rev-parse --short HEAD)"
git diff --quiet HEAD -- . ':!sidecar/package-lock.json' || echo "warning: uncommitted changes are NOT in the package" >&2

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
git archive HEAD | tar -x -C "$work"

pkg="$work/kakao-beeper-macos"
mkdir -p "$pkg/src" "$pkg/sidecar" "$pkg/bin"
cp -R "$work/cmd" "$work/pkg" "$work/go.mod" "$work/go.sum" "$pkg/src/"
cp "$work/sidecar/sidecar.mjs" "$work/sidecar/package.json" "$work/sidecar/package-lock.json" "$pkg/sidecar/"
cp "$work/macos/install.sh" "$work/macos/kakao-beeper" "$pkg/"
cp "$work/macos/README.md" "$pkg/README.md"
chmod 755 "$pkg/install.sh" "$pkg/kakao-beeper"
echo "$version" >"$pkg/VERSION"

if [ -n "${KB_BIN_DIR:-}" ]; then
    for arch in arm64 amd64; do
        f="$KB_BIN_DIR/mautrix-kakaotalk-darwin-$arch"
        [ -f "$f" ] || { echo "missing $f" >&2; exit 1; }
        cp "$f" "$pkg/bin/"
        chmod 755 "$pkg/bin/mautrix-kakaotalk-darwin-$arch"
    done
    kind="prebuilt"
else
    kind="source"
fi

mkdir -p "$out"
tarball="$out/kakao-beeper-macos-$version-$kind.tar.gz"
tar -czf "$tarball" -C "$work" kakao-beeper-macos
echo "$tarball ($(du -h "$tarball" | cut -f1))"
