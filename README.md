# kakao-beeper

KakaoTalk in Beeper, self-hosted. A [mautrix-go bridgev2](https://pkg.go.dev/maunium.net/go/mautrix/bridgev2)
connector (`cmd/mautrix-kakaotalk`, `pkg/connector`) drives a small Node sidecar
(`sidecar/sidecar.mjs`) that holds a KakaoTalk LOCO session through
[agent-messenger](https://github.com/agent-messenger/agent-messenger).

```
Beeper  <--websocket-->  mautrix-kakaotalk (Go)  <--JSON lines/stdio-->  sidecar.mjs (Node)  <--LOCO-->  KakaoTalk
```

**Unofficial.** LOCO access breaks Kakao's terms of service and may get your account limited.
This project is not affiliated with, endorsed by or supported by Kakao Corp. or Beeper;
"KakaoTalk" and its logo are trademarks of Kakao Corp. Use it at your own risk.

Platforms: Linux (systemd, below) and macOS (`macos/`, with a guided installer).

## Device slots

The session uses the **tablet slot** (Android sub-device), so the phone and the
KakaoTalk PC app (PC slot) stay logged in. Never log in with
`--device-type pc` or `--force`.

Only one LOCO connection per device can exist. While the bridge runs, the
`agent-kakaotalk` CLI shares the same device and will kick the bridge's session
(it reconnects after 10 s). Use Beeper (or the Beeper MCP) instead of the CLI
while the bridge is up.

## Setup

1. Log in to KakaoTalk once (password and phone passcode stay in your terminal):
   `agent-kakaotalk auth login`
2. Run the bridge: `scripts/run.sh`. The first run logs `bbctl` in, registers
   `sh-kakaotalk` with Beeper and writes `~/.local/share/kakao-beeper/config.yaml`.
3. In Beeper, start a chat with `@sh-kakaotalkbot:beeper.local` and send `login`.
   The bridge adopts the agent-kakaotalk credentials and starts syncing chats.

The sidecar copies the credentials to `~/.local/share/kakao-beeper/agent-messenger/`.
LOGINLIST only returns chats changed since the saved sync state, so the sidecar
drops that state on every start and always receives the full chat list.

## macOS

`macos/` holds an installer for other people's Macs (launchd instead of systemd, downloads
Node and `bbctl` itself, guided sign-in). `scripts/package-macos.sh` builds the tarball to send;
`.github/workflows/release-macos.yml` builds the macOS binaries on a macOS runner (the bridge
needs cgo for sqlite, so it can't be cross-compiled from Linux). See `macos/README.md`, which is
the file the recipient reads.

## Autostart in every session

After the first `scripts/run.sh` setup, stop it (Ctrl+C) and run `scripts/install-service.sh`.
It builds the bridge and installs `contrib/systemd/kakao-beeper.service` into
`~/.config/systemd/user` (pointing at this checkout), wanted by both `graphical-session.target`
and `beeper.service`: the bridge comes up with every graphical session and whenever Beeper is
started. Closing Beeper leaves it running; it stops with the session.

- Logs: `journalctl --user -u kakao-beeper -f` (also `~/.local/share/kakao-beeper/logs/bridge.log`)
- After pulling changes: `go build -tags goolm -o mautrix-kakaotalk ./cmd/mautrix-kakaotalk && systemctl --user restart kakao-beeper`
- Remove: `systemctl --user disable --now kakao-beeper && rm ~/.config/systemd/user/kakao-beeper.service`

## What's bridged

| KakaoTalk → Beeper | Beeper → KakaoTalk |
| --- | --- |
| text, replies, photos, multi-photos, video, audio, files | text, emotes, images (jpeg/png) |
| emoticons/stickers (as a notice), bot cards (as text) | read receipts, typing |
| read receipts, forward backfill (30 messages per new chat) | |

Not yet: reactions, edits/deletes, member join/leave sync, backward backfill,
sending files other than images.

## Connection handling

agent-messenger drops its session when the LOCO socket closes and only logs in again on
the next API call, so the sidecar takes care of staying connected:

- socket closed (sleep, network change) → `disconnected` event, reconnect with a 3 s → 60 s
  backoff; on success the bridge resyncs chats and forward-backfills what was missed
- a CHATINFO round trip on the memo chat every 90 s catches half-open sockets (pings inside
  agent-messenger are fire-and-forget) and closes the session so the loop above runs
- kicked by another device → sidecar exits 3, the bridge logs in again after one minute
- token rejected by KakaoTalk → sidecar exits 78, bridge state `BAD_CREDENTIALS`, no retries
  until `agent-kakaotalk auth login` + restart
- a failed start (offline) is retried after 10 s, 20 s, 40 s … up to 5 min

## Drafting before sending

If you use an AI assistant with the Beeper Desktop MCP server, have it draft instead of send:
the MCP `focus_app` tool with `chatID` + `draftText` opens the chat with the text in the
composer, and you review and press Enter yourself.

## Credits

Built on [mautrix-go](https://github.com/mautrix/go) (bridgev2) and
[agent-messenger](https://github.com/agent-messenger/agent-messenger), whose KakaoTalk LOCO client
builds on earlier protocol work (openkakao, node-kakao). Self-hosting uses Beeper's
[bridge-manager](https://github.com/beeper/bridge-manager) (`bbctl`). See `THIRD_PARTY_NOTICES.md`.
