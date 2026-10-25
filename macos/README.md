# KakaoTalk in Beeper — macOS

Brings your KakaoTalk chats into [Beeper](https://www.beeper.com): read them and reply from
Beeper like any other network. It runs on your own Mac, signed in to *your* KakaoTalk account;
nothing goes through anyone else's server except Beeper's own.

```
Beeper  <->  bridge on your Mac  <->  KakaoTalk
```

## Before you install

- **Unofficial.** It talks to KakaoTalk the way a second device would, which Kakao's terms of
  service don't allow. Accounts can get limited. Use it at your own risk.
- **It takes your tablet slot.** KakaoTalk allows one phone, one PC and one tablet at a time. The
  bridge signs in as the *tablet*. Your phone and the KakaoTalk Mac app stay signed in; a
  KakaoTalk you use on an iPad or Android tablet would be signed out.
- **Your Mac has to be on.** Chats sync while the Mac is awake and you're logged in. The bridge
  starts at login and reconnects by itself after sleep or a network change.
- **Credentials stay on your Mac** (`~/.config/agent-messenger`, readable only by you). Your
  KakaoTalk password is typed into the Terminal and never sent to Beeper.
- You need a **Beeper account** and Beeper Desktop installed, macOS 13 or newer.

## Install

Open Terminal in the folder you unpacked and run:

```
bash install.sh
```

It installs into `~/.local/share/kakao-beeper` (nothing needs `sudo`), then walks you through:

1. **KakaoTalk sign-in** — your KakaoTalk email and password. The Terminal then shows a short
   code; KakaoTalk on your phone asks you to approve a new device with that code. (If KakaoTalk
   for Mac is installed, the email may be pre-filled from it.)
2. **Beeper sign-in** — email and the code Beeper sends you.
3. Starts the bridge and adds a login item so it starts every time you log in.

Last step, in Beeper Desktop: start a chat with **`@sh-kakaotalkbot:beeper.local`** and send
`login`. Your KakaoTalk chats show up within a minute or two.

If the package has no prebuilt program for your Mac, the installer builds it and asks for two
things first: `brew install go` and `xcode-select --install`.

## Everyday use

```
kakao-beeper status      is it running and signed in
kakao-beeper doctor      full check-up; tells you what to do about anything that fails
kakao-beeper logs        follow the logs
kakao-beeper restart
kakao-beeper setup       run the guided steps again
kakao-beeper uninstall   remove everything
```

If the command isn't found, open a new Terminal window (the installer offers to add
`~/.local/bin` to your PATH) or run `~/.local/bin/kakao-beeper`.

macOS may show a notification about a new background item — that is the login item above.

## Updating

Unpack the new package, then in its folder: `bash install.sh --no-setup` and
`kakao-beeper restart`. Your sign-ins, chats and settings are kept.

## What works

KakaoTalk → Beeper: text, replies, photos, video, audio, files, read receipts; stickers show as
a notice. Beeper → KakaoTalk: text, images, read receipts, typing. Not yet: reactions,
edits/deletes, member join/leave, sending files other than images.

## Troubleshooting

- First: `kakao-beeper doctor`.
- *Nothing shows up in Beeper*: make sure you sent `login` to the bot; then `kakao-beeper logs`.
- *"KakaoTalk session: BAD_CREDENTIALS"*: KakaoTalk signed the bridge out.
  `kakao-beeper login-kakao`, then `kakao-beeper restart`.
- *"kakao-kicked"*: another device signed in as the tablet. The bridge signs back in after a
  minute. If you want to use that other device instead, `kakao-beeper stop` first.
- *Phone says "new device"*: that is the bridge (tablet). Expected.
- *Disconnected after sleep*: it reconnects on its own once the network is back; give it a minute.

For anyone helping you with Claude Code or another coding assistant: `CLAUDE.md` in this folder
explains the whole setup to it.
