# Third-party notices

kakao-beeper links against, installs or downloads the following. Each keeps its own license;
nothing here changes it.

## Linked into the bridge binary (Go modules)

| Module | License | Source |
| --- | --- | --- |
| maunium.net/go/mautrix (mautrix-go, bridgev2) | MPL-2.0 | https://github.com/mautrix/go |
| go.mau.fi/util | MPL-2.0 | https://github.com/mautrix/go-util |
| github.com/rs/zerolog | MIT | https://github.com/rs/zerolog |
| github.com/mattn/go-sqlite3 | MIT | https://github.com/mattn/go-sqlite3 |
| github.com/coder/websocket | ISC | https://github.com/coder/websocket |
| other indirect modules | see `go.sum` and each module's repository | |

The MPL-2.0 modules are used unmodified; their complete source is available at the links above
and through the Go module proxy at the versions pinned in `go.mod`.

## Installed at setup time (not distributed in this repository)

| Package | License | Notes |
| --- | --- | --- |
| [agent-messenger](https://github.com/agent-messenger/agent-messenger) (npm) | MIT | KakaoTalk LOCO client used by the sidecar. Its protocol code credits openkakao (MIT) and node-kakao; see agent-messenger's own notices. |
| [bridge-manager / bbctl](https://github.com/beeper/bridge-manager) | see its repository | Registers the self-hosted bridge with Beeper; downloaded by the macOS installer with a pinned checksum. |
| [Node.js](https://nodejs.org) | MIT and others | Downloaded by the macOS installer when no suitable Node is installed. |

## Trademarks and artwork

"KakaoTalk" and the KakaoTalk app icon (`pkg/connector/kakaotalk-icon.png`, shown as the network
icon in Beeper) are trademarks of Kakao Corp. This project is not affiliated with or endorsed by
Kakao Corp. or Beeper. The icon is used only to identify the network the bridge connects to.
