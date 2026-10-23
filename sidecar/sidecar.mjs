// KakaoTalk sidecar for mautrix-kakaotalk.
//
// Owns a single LOCO session (agent-messenger, tablet slot) and speaks
// newline-delimited JSON on stdio:
//   request  {"id": 1, "method": "getChats", "params": {...}}
//   response {"id": 1, "result": ...} | {"id": 1, "error": {"code", "message"}}
//   event    {"event": "message", "data": {...}}
// Logs go to stderr so stdout stays a clean protocol stream.
//
// The sidecar keeps its own agent-messenger config dir (KAKAO_SIDECAR_STATE_DIR),
// independent of the agent-kakaotalk CLI. On first start the credentials are
// copied over from the CLI's config dir. LOGINLIST only returns chats changed
// since the saved sync state, so start() drops that state to always get the
// full chat list (the bridge dedupes known chats itself).

import { copyFileSync, existsSync, mkdirSync, readdirSync, rmSync } from 'node:fs'
import { homedir } from 'node:os'
import { join } from 'node:path'
import { createInterface } from 'node:readline'

const CREDENTIALS_FILE = 'kakaotalk-credentials.json'
const stateDir = process.env.KAKAO_SIDECAR_STATE_DIR ?? join(homedir(), '.local/share/kakao-beeper/agent-messenger')
const cliDir = process.env.KAKAO_CLI_CONFIG_DIR ?? join(homedir(), '.config/agent-messenger')
mkdirSync(stateDir, { recursive: true, mode: 0o700 })
if (!existsSync(join(stateDir, CREDENTIALS_FILE)) && existsSync(join(cliDir, CREDENTIALS_FILE))) {
  copyFileSync(join(cliDir, CREDENTIALS_FILE), join(stateDir, CREDENTIALS_FILE))
}
process.env.AGENT_MESSENGER_CONFIG_DIR = stateDir

// Imported after the env var is set so agent-messenger picks up stateDir.
const { KakaoTalkClient, KakaoTalkListener } = await import('agent-messenger/kakaotalk')
const { Long } = await import('bson')

let client = null
let listener = null

const write = (obj) => process.stdout.write(JSON.stringify(obj) + '\n')
const log = (...args) => console.error('[sidecar]', ...args)
const emit = (event, data) => write({ event, data })

// LOCO values can carry bigints; make everything JSON-safe.
const plain = (value) => JSON.parse(JSON.stringify(value ?? null, (_k, v) => (typeof v === 'bigint' ? v.toString() : v)))

function dropSyncState() {
  for (const name of readdirSync(stateDir)) {
    if (name.startsWith('kakaotalk-sync-state-')) rmSync(join(stateDir, name), { force: true })
  }
}

async function start() {
  if (client) return plain(await client.getProfile())
  dropSyncState()
  client = await new KakaoTalkClient().login()
  listener = new KakaoTalkListener(client)
  listener.on('connected', (info) => emit('connected', plain(info)))
  listener.on('disconnected', () => emit('disconnected', {}))
  listener.on('error', (err) => emit('error', { message: err?.message ?? String(err) }))
  listener.on('message', (evt) => emit('message', plain(evt)))
  listener.on('read', (evt) => emit('read', plain(evt)))
  listener.on('member_joined', (evt) => emit('member_joined', plain(evt)))
  listener.on('member_left', (evt) => emit('member_left', plain(evt)))
  await listener.start()
  return plain(await client.getProfile())
}

function need() {
  if (!client) throw Object.assign(new Error('sidecar not started'), { code: 'not_started' })
  return client
}

// agent-messenger's getMembers double-reads CHATINFO/GETMEM and rejects groups
// whose member records don't exactly match the active member count, which some
// groups never do. Fall back to a single GETMEM read for those.
async function getMembers(chatId) {
  try {
    return await need().getMembers(chatId)
  } catch (err) {
    if (!String(err?.message).includes('member_snapshot_changed_or_incomplete')) throw err
  }
  const session = await need().acquireSession()
  const res = await session.getAllMembers(Long.fromString(chatId))
  if (typeof res.body.status === 'number' && res.body.status !== 0) {
    throw Object.assign(new Error(`GETMEM failed: ${res.body.status}`), { code: 'get_members_failed' })
  }
  return (res.body.members ?? []).map((m) => ({
    user_id: String(m.userId),
    nickname: typeof m.nickName === 'string' ? m.nickName : '',
    profile_image_url: m.profileImageUrl ?? m.pi ?? null,
  }))
}

const methods = {
  start,
  ping: async () => 'pong',
  getProfile: async () => plain(await need().getProfile()),
  getChats: async (p) => plain(await need().getChats({ all: p.all ?? true, resolveTitles: p.resolve_titles ?? false })),
  getChat: async (p) => plain(await need().getChat(p.chat_id)),
  getMembers: async (p) => plain(await getMembers(p.chat_id)),
  getMessages: async (p) => plain(await need().getMessages(p.chat_id, { count: p.count ?? 20, from: p.from })),
  getMessagePage: async (p) => plain(await need().getMessagePage(p.chat_id, { count: p.count ?? 50, from: p.from })),
  sendMessage: async (p) =>
    plain(await need().sendMessage(p.chat_id, p.text, p.reply_to ? { replyTo: p.reply_to } : undefined)),
  sendPhoto: async (p) => plain(await need().sendPhoto(p.chat_id, Buffer.from(p.data_b64, 'base64'), p.filename)),
  sendTyping: async (p) => plain(await need().sendTyping(p.chat_id)),
  markRead: async (p) => plain(await need().markRead(p.chat_id, p.log_id)),
}

function shutdown(code = 0) {
  try { listener?.stop() } catch {}
  try { client?.close() } catch {}
  process.exit(code)
}

process.on('SIGTERM', () => shutdown(0))
process.on('SIGINT', () => shutdown(130))
process.on('unhandledRejection', (err) => log('unhandled rejection:', err?.message ?? err))

const rl = createInterface({ input: process.stdin })
rl.on('line', async (line) => {
  if (!line.trim()) return
  let req
  try {
    req = JSON.parse(line)
  } catch {
    log('bad request line')
    return
  }
  const fn = methods[req.method]
  if (!fn) {
    write({ id: req.id, error: { code: 'unknown_method', message: `unknown method ${req.method}` } })
    return
  }
  try {
    write({ id: req.id, result: (await fn(req.params ?? {})) ?? null })
  } catch (err) {
    write({ id: req.id, error: { code: err?.code ?? 'error', message: err?.message ?? String(err) } })
  }
})
// The bridge owns our lifetime: exit when it closes our stdin.
rl.on('close', () => shutdown(0))
