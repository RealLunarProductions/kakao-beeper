#!/usr/bin/env bash
# Move the running bridge from this machine's user service to a server.
#   bash server/migrate.sh ubuntu@1.2.3.4 [-i ~/.ssh/key]
# Builds nothing: run server/build.sh first. The local data dir is renamed to
# kakao-beeper.migrated-<date> so the laptop service can't start a second copy
# (two copies of the same bridge kick each other off Beeper and KakaoTalk).
# Undo: bash server/migrate.sh --rollback ubuntu@1.2.3.4 [-i key]
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
data="${KAKAO_BEEPER_DATA:-$HOME/.local/share/kakao-beeper}"

rollback=0
[ "${1:-}" = --rollback ] && { rollback=1; shift; }
host="${1:?usage: migrate.sh [--rollback] user@host [-i key]}"; shift
ssh_opts=(-o StrictHostKeyChecking=accept-new -o ConnectTimeout=15 "$@")
r() { ssh "${ssh_opts[@]}" "$host" "$@"; }

if [ "$rollback" = 1 ]; then
    r 'sudo systemctl disable --now kakao-beeper'
    last="$(ls -d "$data".migrated-* 2>/dev/null | tail -1)"
    [ -n "$last" ] && [ ! -e "$data" ] && mv "$last" "$data"
    systemctl --user enable --now kakao-beeper
    systemctl --user --no-pager status kakao-beeper | head -5
    exit 0
fi

bundle="$here/dist/kakao-beeper-server.tar.gz"
[ -f "$bundle" ] || { echo "no $bundle: run server/build.sh first" >&2; exit 1; }
[ -f "$data/config.yaml" ] || { echo "no $data/config.yaml" >&2; exit 1; }

echo "== Checking $host"
r 'uname -m; sudo -n true && echo sudo ok'

echo "== Installing bundle on the server"
scp "${ssh_opts[@]}" "$bundle" "$host:/tmp/kakao-beeper-server.tar.gz"
r 'rm -rf /tmp/kb && mkdir /tmp/kb && tar -xzf /tmp/kakao-beeper-server.tar.gz -C /tmp/kb && sudo bash /tmp/kb/server/install.sh'

echo "== Stopping the local bridge"
systemctl --user stop kakao-beeper

echo "== Copying the bridge data (config, database, KakaoTalk session)"
tar -C "$data" --exclude=./logs --exclude='./config.yaml.broken-*' --exclude='*.bak-*' -czf - . |
    r 'sudo tar -xzf - -C /var/lib/kakao-beeper && cd /var/lib/kakao-beeper && sudo sed -i \
        -e "s#^\(    node_path:\).*#\1 /opt/node/bin/node#" \
        -e "s#^\(    sidecar_script:\).*#\1 /opt/kakao-beeper/sidecar/sidecar.mjs#" \
        -e "s#^\(    state_dir:\).*#\1 /var/lib/kakao-beeper/agent-messenger#" config.yaml &&
        sudo chown -R kakao:kakao /var/lib/kakao-beeper && sudo chmod -R go-rwx /var/lib/kakao-beeper &&
        sudo sed -n "1,12p" config.yaml | grep -E "node_path|sidecar_script|state_dir"'

echo "== Starting the bridge on the server"
r 'sudo systemctl restart kakao-beeper && sleep 20 && systemctl --no-pager status kakao-beeper | head -6 &&
   sudo journalctl -u kakao-beeper -n 25 --no-pager -o cat'

echo "== Retiring the local copy"
systemctl --user disable kakao-beeper
mv "$data" "$data.migrated-$(date +%Y%m%d-%H%M%S)"
echo "Done. Server logs: ssh $host sudo journalctl -u kakao-beeper -f"
