#!/usr/bin/env bash
# Run on the server as root from the unpacked bundle: sudo bash install.sh
# Installs Node 22, the bridge in /opt/kakao-beeper, data in /var/lib/kakao-beeper
# and kakao-beeper.service. Safe to re-run for updates (keeps the data dir).
set -euo pipefail
[ "$(id -u)" = 0 ] || { echo "run as root (sudo bash install.sh)" >&2; exit 1; }
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
[ "$(uname -m)" = x86_64 ] || { echo "this bundle is x86_64 only" >&2; exit 1; }

# Node >= 22.13 from nodejs.org, checked against the published SHA-256.
if ! /opt/node/bin/node -e 'const [a,b]=process.versions.node.split(".").map(Number);process.exit(a>22||(a==22&&b>=13)?0:1)' 2>/dev/null; then
    echo "== Installing Node 22"
    base=https://nodejs.org/dist/latest-v22.x
    tmp="$(mktemp -d)"
    curl -fsSL "$base/SHASUMS256.txt" -o "$tmp/SHASUMS256.txt"
    file="$(grep -o 'node-v[0-9.]*-linux-x64.tar.xz' "$tmp/SHASUMS256.txt" | head -1)"
    curl -fsSL "$base/$file" -o "$tmp/$file"
    (cd "$tmp" && grep " $file\$" SHASUMS256.txt | sha256sum -c -)
    rm -rf /opt/node && mkdir -p /opt/node
    tar -xJf "$tmp/$file" -C /opt/node --strip-components=1
    rm -rf "$tmp"
fi
/opt/node/bin/node --version

# A 1 GB VM has no swap by default; add 1 GB so a spike can't OOM the box.
if ! swapon --show | grep -q . && [ ! -f /swapfile ]; then
    echo "== Adding 1 GB swap"
    fallocate -l 1G /swapfile && chmod 600 /swapfile && mkswap /swapfile >/dev/null && swapon /swapfile
    echo '/swapfile none swap sw 0 0' >>/etc/fstab
fi

id kakao >/dev/null 2>&1 || useradd --system --home-dir /var/lib/kakao-beeper --shell /usr/sbin/nologin kakao
install -d -o kakao -g kakao -m 700 /var/lib/kakao-beeper /var/lib/kakao-beeper/logs

echo "== Installing bridge ($(cat "$here/COMMIT"))"
systemctl stop kakao-beeper 2>/dev/null || true
rm -rf /opt/kakao-beeper.new && mkdir -p /opt/kakao-beeper.new
cp -a "$here/mautrix-kakaotalk" "$here/sidecar" "$here/COMMIT" /opt/kakao-beeper.new/
rm -rf /opt/kakao-beeper && mv /opt/kakao-beeper.new /opt/kakao-beeper
chown -R root:root /opt/kakao-beeper

install -m 644 "$here/kakao-beeper.service" /etc/systemd/system/kakao-beeper.service
systemctl daemon-reload
systemctl enable kakao-beeper >/dev/null
if [ -f /var/lib/kakao-beeper/config.yaml ]; then
    systemctl restart kakao-beeper
    sleep 5
    systemctl --no-pager status kakao-beeper | head -8
else
    echo "== Installed. No config.yaml yet: copy the data dir over, then systemctl start kakao-beeper"
fi
