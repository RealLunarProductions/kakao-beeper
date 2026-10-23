#!/usr/bin/env bash
# Run the KakaoTalk -> Beeper bridge in the foreground.
# First run: logs bbctl in (if needed), registers sh-kakaotalk with Beeper and
# writes the config to $KAKAO_BEEPER_DATA (default ~/.local/share/kakao-beeper).
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
data="${KAKAO_BEEPER_DATA:-$HOME/.local/share/kakao-beeper}"
mkdir -p "$data"
cd "$data"

if ! bbctl whoami >/dev/null 2>&1; then
    echo "== Logging bbctl in to Beeper"
    bbctl login
fi

if [ ! -f config.yaml ]; then
    echo "== Registering sh-kakaotalk with Beeper and generating config.yaml"
    bbctl config --type bridgev2 sh-kakaotalk -o config.yaml
fi

if ! grep -q '^network:' config.yaml; then
    cat >>config.yaml <<EOF

# Network-specific config options
network:
    node_path: $(command -v node)
    sidecar_script: $here/sidecar/sidecar.mjs
    state_dir: $data/agent-messenger
    initial_backfill: 30
EOF
fi

if [ ! -d "$here/sidecar/node_modules" ]; then
    npm install --prefix "$here/sidecar" --ignore-scripts --no-audit --no-fund
fi
if [ ! -x "$here/mautrix-kakaotalk" ]; then
    (cd "$here" && go build -tags goolm -o mautrix-kakaotalk ./cmd/mautrix-kakaotalk)
fi

echo "== Starting bridge (Ctrl+C to stop). In Beeper, DM @sh-kakaotalkbot:beeper.local and send: login"
exec "$here/mautrix-kakaotalk" -c config.yaml
