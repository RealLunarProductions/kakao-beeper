#!/usr/bin/env bash
# Install kakao-beeper.service so the bridge starts with every graphical session
# (Noctalia and all rices) and whenever beeper.service starts.
# Run scripts/run.sh once first: it logs bbctl in and writes the config.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
unit_src="$here/contrib/systemd/kakao-beeper.service"
unit_dir="$HOME/.config/systemd/user"
unit_dst="$unit_dir/kakao-beeper.service"

if [ ! -f "$HOME/.local/share/kakao-beeper/config.yaml" ]; then
    echo "No bridge config yet: run scripts/run.sh once first." >&2
    exit 1
fi

# Only one bridge may be connected to Beeper at a time.
if ! systemctl --user is-active --quiet kakao-beeper.service && pgrep -f "mautrix-kakaotalk -c" >/dev/null; then
    echo "A bridge started by run.sh is still running. Stop it (Ctrl+C in its terminal) and re-run this." >&2
    exit 1
fi

echo "== Building bridge"
(cd "$here" && go build -tags goolm -o mautrix-kakaotalk ./cmd/mautrix-kakaotalk)

echo "== Installing $unit_dst"
mkdir -p "$unit_dir"
install -m 644 "$unit_src" "$unit_dst"
if command -v chezmoi >/dev/null && chezmoi managed --include=files | grep -q '^\.config/systemd/user/beeper\.service$'; then
    chezmoi add "$unit_dst"
    echo "   added to chezmoi"
fi

systemctl --user daemon-reload
systemctl --user enable kakao-beeper.service
systemctl --user restart kakao-beeper.service
sleep 5
systemctl --user --no-pager status kakao-beeper.service | head -12
