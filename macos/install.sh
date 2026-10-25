#!/bin/bash
# Installs the KakaoTalk -> Beeper bridge on macOS (Apple Silicon or Intel).
#
#   bash install.sh              install, then walk through the sign-ins
#   bash install.sh --no-setup   only install the files
#
# Everything lives under ~/.local/share/kakao-beeper (program, config, data) plus
# ~/.local/bin/kakao-beeper and one login item in ~/Library/LaunchAgents.
# Written for macOS's stock bash 3.2.
set -euo pipefail

NODE_VERSION="v22.23.3"
BBCTL_VERSION="v0.15.0"
# SHA-256 of the bbctl release assets (from the release's sha256sums.txt).
BBCTL_SHA_arm64="4ed8dcd4bfda2500fef7258d6109dfc200a9d7a4046c878d9749962d1219671c"
BBCTL_SHA_amd64="887b8a344c2782c150bb84aecd6c144f5b8d028bf9960622cf497730fb5dbf2c"
NODE_MIN_MAJOR=22
NODE_MIN_MINOR=13

PKG="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DATA="${KAKAO_BEEPER_DATA:-$HOME/.local/share/kakao-beeper}"
APP="$DATA/app"
BIN_DIR="$HOME/.local/bin"
RUN_SETUP=1
[ "${1:-}" = "--no-setup" ] && RUN_SETUP=0

say() { printf '%s\n' "$*"; }
step() { printf '\n== %s\n' "$*"; }
die() { printf '\nerror: %s\n' "$*" >&2; exit 1; }
sha256() { if command -v shasum >/dev/null 2>&1; then shasum -a 256 "$1" | cut -d' ' -f1; else sha256sum "$1" | cut -d' ' -f1; fi; }

# --- preflight ---------------------------------------------------------------
step "Checking this Mac"
[ "$(uname -s)" = "Darwin" ] || die "this installer is for macOS."
macos="$(sw_vers -productVersion)"
[ "${macos%%.*}" -ge 13 ] || die "macOS 13 or newer is required (this is $macos)."
# uname -m says x86_64 in a Rosetta terminal even on Apple Silicon; ask the CPU.
if [ "$(sysctl -n hw.optional.arm64 2>/dev/null || echo 0)" = "1" ]; then ARCH=arm64; else ARCH=amd64; fi
say "macOS $macos, $ARCH"
if [ ! -d /Applications/Beeper.app ] && [ ! -d "$HOME/Applications/Beeper.app" ] && ! ls /Applications 2>/dev/null | grep -qi beeper; then
    say "note: Beeper Desktop isn't in /Applications. Install it from https://www.beeper.com before the last step."
fi
mkdir -p "$APP/bin" "$BIN_DIR" "$DATA/logs"

# --- node --------------------------------------------------------------------
node_ok() {
    [ -x "$1" ] && "$1" -e "const [a,b]=process.versions.node.split('.').map(Number);process.exit(a>$NODE_MIN_MAJOR||(a===$NODE_MIN_MAJOR&&b>=$NODE_MIN_MINOR)?0:1)" 2>/dev/null
}

step "Node.js (the KakaoTalk connection runs on it)"
NODE=""
for cand in "$(command -v node 2>/dev/null || true)" /opt/homebrew/bin/node /usr/local/bin/node "$APP/node/bin/node"; do
    if [ -n "$cand" ] && node_ok "$cand"; then NODE="$cand"; break; fi
done
if [ -z "$NODE" ]; then
    case "$ARCH" in arm64) nodearch=arm64 ;; *) nodearch=x64 ;; esac
    base="node-$NODE_VERSION-darwin-$nodearch"
    say "No Node.js $NODE_MIN_MAJOR.$NODE_MIN_MINOR+ found; downloading the official build ($base)"
    work="$(mktemp -d)"
    curl -fL --retry 3 -o "$work/node.tar.gz" "https://nodejs.org/dist/$NODE_VERSION/$base.tar.gz"
    curl -fsSL --retry 3 -o "$work/SHASUMS256.txt" "https://nodejs.org/dist/$NODE_VERSION/SHASUMS256.txt"
    want="$(grep " $base.tar.gz\$" "$work/SHASUMS256.txt" | cut -d' ' -f1)"
    [ -n "$want" ] && [ "$(sha256 "$work/node.tar.gz")" = "$want" ] || die "Node.js download failed its checksum."
    rm -rf "$APP/node" && mkdir -p "$APP/node"
    tar -xzf "$work/node.tar.gz" -C "$APP/node" --strip-components=1
    rm -rf "$work"
    NODE="$APP/node/bin/node"
    node_ok "$NODE" || die "downloaded Node.js does not run."
fi
printf '%s' "$NODE" >"$APP/node-path"
say "using $NODE ($("$NODE" --version))"

# --- bbctl (Beeper's self-hosted bridge manager) -----------------------------
step "bbctl (Beeper bridge manager $BBCTL_VERSION)"
if [ -n "${KB_BBCTL:-}" ]; then
    cp "$KB_BBCTL" "$APP/bin/bbctl"
else
    case "$ARCH" in arm64) want="$BBCTL_SHA_arm64" ;; *) want="$BBCTL_SHA_amd64" ;; esac
    if [ ! -x "$APP/bin/bbctl" ] || [ "$(sha256 "$APP/bin/bbctl")" != "$want" ]; then
        curl -fL --retry 3 -o "$APP/bin/bbctl.new" "https://github.com/beeper/bridge-manager/releases/download/$BBCTL_VERSION/bbctl-macos-$ARCH"
        [ "$(sha256 "$APP/bin/bbctl.new")" = "$want" ] || { rm -f "$APP/bin/bbctl.new"; die "bbctl download failed its checksum."; }
        mv "$APP/bin/bbctl.new" "$APP/bin/bbctl"
    fi
fi
chmod 755 "$APP/bin/bbctl"

# --- bridge binary -----------------------------------------------------------
step "Bridge program"
prebuilt="$PKG/bin/mautrix-kakaotalk-darwin-$ARCH"
if [ -f "$prebuilt" ]; then
    say "using the prebuilt binary"
    cp "$prebuilt" "$APP/bin/mautrix-kakaotalk.new"
else
    [ -d "$PKG/src" ] || die "this package has no prebuilt binary for $ARCH and no source to build from."
    command -v go >/dev/null 2>&1 || die "no prebuilt binary in this package and Go isn't installed. Run: brew install go   (or ask for a package that includes the binary)"
    xcode-select -p >/dev/null 2>&1 || die "building needs Apple's command line tools. Run: xcode-select --install   then re-run this."
    say "building from source (needs Go and Apple's command line tools; takes a couple of minutes)"
    (cd "$PKG/src" && CGO_ENABLED=1 go build -tags goolm -trimpath -ldflags "-s -w" -o "$APP/bin/mautrix-kakaotalk.new" ./cmd/mautrix-kakaotalk)
fi
chmod 755 "$APP/bin/mautrix-kakaotalk.new"
mv "$APP/bin/mautrix-kakaotalk.new" "$APP/bin/mautrix-kakaotalk"
# Files from a browser download carry Gatekeeper's quarantine flag; Apple Silicon also
# insists that every binary is at least ad-hoc signed.
xattr -dr com.apple.quarantine "$APP/bin" 2>/dev/null || true
command -v codesign >/dev/null 2>&1 && codesign --force --sign - "$APP/bin/mautrix-kakaotalk" "$APP/bin/bbctl" 2>/dev/null || true

# --- sidecar -----------------------------------------------------------------
step "KakaoTalk connection (sidecar)"
mkdir -p "$APP/sidecar"
cp "$PKG/sidecar/sidecar.mjs" "$PKG/sidecar/package.json" "$PKG/sidecar/package-lock.json" "$APP/sidecar/"
(
    cd "$APP/sidecar"
    # npm ships with Node; make sure it finds the same node. Optional packages are other
    # chat networks' native code that this bridge doesn't use.
    PATH="$(dirname "$NODE"):$PATH" npm ci --omit=dev --omit=optional --ignore-scripts --no-audit --no-fund --loglevel=error
)
(cd "$APP/sidecar" && "$NODE" --input-type=module -e "await import('agent-messenger/kakaotalk'); await import('bson')") \
    || die "the sidecar's packages did not install correctly."
say "sidecar packages ok"

# --- control script ----------------------------------------------------------
step "kakao-beeper command"
cp "$PKG/kakao-beeper" "$BIN_DIR/kakao-beeper"
chmod 755 "$BIN_DIR/kakao-beeper"
case ":$PATH:" in
    *":$BIN_DIR:"*) ;;
    *)
        say "$BIN_DIR is not on your PATH, so 'kakao-beeper' won't be found by name yet."
        if [ -t 0 ]; then
            read -r -p "Add it to ~/.zshrc? [Y/n] " reply || reply=""
            case "$reply" in
                [nN]*) say "Skipped. Run it as $BIN_DIR/kakao-beeper" ;;
                *) printf '\nexport PATH="$HOME/.local/bin:$PATH"\n' >>"$HOME/.zshrc"; say "Added. Open a new Terminal window to pick it up." ;;
            esac
        fi
        ;;
esac

say ""
say "Installed."
if [ "$RUN_SETUP" = 1 ]; then
    exec "$BIN_DIR/kakao-beeper" setup
else
    say "Next: $BIN_DIR/kakao-beeper setup"
fi
