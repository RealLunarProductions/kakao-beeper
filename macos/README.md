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
  starts at login and reconnects after sleep.
- **Credentials stay on your Mac** (`~/.config/agent-messenger`, readable only by you). Your
  KakaoTalk password is typed into the Terminal and never sent to Beeper.
- You need a **Beeper account** and Beeper Desktop installed, macOS 13 or newer.

## Install

Open Terminal in the folder you unpacked and run:

```
bash install.sh
```

It installs into `~/.local/share/kakao-beeper` (nothing needs `sudo`), then walks you through:

1. **KakaoTalk sign-in** — email and password (KakaoTalk may also ask for a code on your phone).
2. **Beeper sign-in** — email and the code Beeper sends you.
3. Starts the bridge and adds a login item so it starts every time you log in.

Last step, in Beeper Desktop: start a chat with **`@sh-kakaotalkbot:beeper.local`** and send
`login`. Your KakaoTalk chats show up within a minute or two.

If the package has no prebuilt program for your Mac, the installer builds it and asks for two
things first: `brew install go` and `xcode-select --install`.

## Everyday use

```
kakao-beeper status      is it running and signed in
kakao-beeper logs        follow the logs
kakao-beeper restart
kakao-beeper setup       run the guided steps again
kakao-beeper uninstall   remove everything
```

If the command isn't found, open a new Terminal window (the installer offers to add
`~/.local/bin` to your PATH) or run `~/.local/bin/kakao-beeper`.

macOS may show "Background items added" for `kakao-beeper` — that is the login item above.

## What works

KakaoTalk → Beeper: text, replies, photos, video, audio, files, read receipts; stickers show as
a notice. Beeper → KakaoTalk: text, images, read receipts, typing. Not yet: reactions,
edits/deletes, member join/leave, sending files other than images.

## Troubleshooting

- *Nothing shows up in Beeper*: `kakao-beeper status`, then `kakao-beeper logs`. Make sure you sent
  `login` to the bot.
- *KakaoTalk signed itself out*: `kakao-beeper login-kakao`, then `kakao-beeper restart`.
- *Phone says "new device"*: that is the bridge (tablet). Expected.
- While the bridge runs, don't use another tool that signs in as the same tablet — they kick each
  other out.
