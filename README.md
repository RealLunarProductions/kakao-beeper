# kakao-beeper

KakaoTalk in Beeper, self-hosted. A [mautrix-go bridgev2](https://pkg.go.dev/maunium.net/go/mautrix/bridgev2)
connector (`cmd/mautrix-kakaotalk`, `pkg/connector`) drives a small Node sidecar
(`sidecar/sidecar.mjs`) that holds a KakaoTalk LOCO session through
[agent-messenger](https://github.com/agent-messenger/agent-messenger).

```
Beeper  <--websocket-->  mautrix-kakaotalk (Go)  <--JSON lines/stdio-->  sidecar.mjs (Node)  <--LOCO-->  KakaoTalk
```

Unofficial: LOCO access breaks Kakao's terms of service and may get the account limited.

## Device slots

The session uses the **tablet slot** (Android sub-device), so the phone and the
Wine KakaoTalk PC app (PC slot) stay logged in. Never log in with
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

## What's bridged

| KakaoTalk → Beeper | Beeper → KakaoTalk |
| --- | --- |
| text, replies, photos, multi-photos, video, audio, files | text, emotes, images (jpeg/png) |
| emoticons/stickers (as a notice), bot cards (as text) | read receipts, typing |
| read receipts, forward backfill (30 messages per new chat) | |

Not yet: reactions, edits/deletes, member join/leave sync, backward backfill,
sending files other than images.

## Drafting before sending

Once a chat is bridged, Claude drafts into Beeper instead of sending: the Beeper
MCP `focus_app` tool with `chatID` + `draftText` opens the chat with the text in
the composer, and you review and press Enter yourself.
