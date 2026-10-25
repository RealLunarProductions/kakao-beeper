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
//
// Session lifecycle: agent-messenger clears its session when the socket closes
// (sleep, Wi-Fi change) and only reconnects on the next API call, so pushes would
// stop until something is sent. The sidecar therefore reconnects itself with a
// backoff, and runs a periodic LOCO round trip to notice half-open sockets. It
// exits with EXIT_KICKED when another device takes the slot and with
// EXIT_BAD_CREDENTIALS when KakaoTalk rejects the stored token; the bridge reads
// the exit code to decide whether and when to start it again.

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

// Exit codes the bridge understands (pkg/connector/client.go).
const EXIT_KICKED = 3
const EXIT_BAD_CREDENTIALS = 78

const RECONNECT_MIN_MS = 3_000
const RECONNECT_MAX_MS = 60_000
const HEALTH_INTERVAL_MS = 90_000
const HEALTH_TIMEOUT_MS = 20_000

const AUTH_ERROR_CODES = new Set(['invalid_access_token', 'login_rejected', 'not_authenticated', 'missing_token', 'missing_user_id'])

let client = null
let listener = null
let unsubscribeSession = null
let closing = false
let reconnecting = false
let healthTimer = null
// A chat used for the health round trip; the memo chat (나와의 채팅) when there is one.
let healthChatId = null

const write = (obj) => process.stdout.write(JSON.stringify(obj) + '\n')
const log = (...args) => console.error('[sidecar]', ...args)
const emit = (event, data) => write({ event, data })
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

// LOCO values can carry bigints; make everything JSON-safe.
const plain = (value) => JSON.parse(JSON.stringify(value ?? null, (_k, v) => (typeof v === 'bigint' ? v.toString() : v)))

const isAuthError = (err) => AUTH_ERROR_CODES.has(err?.code) || /No KakaoTalk credentials found/.test(err?.message ?? '')
const isTransportError = (err) =>
  /timeout|connection closed|Not connected|ECONN|EPIPE|socket/i.test(err?.message ?? '') || err?.code === 'health_timeout'

function withTimeout(promise, ms, what) {
  let timer
  const timeout = new Promise((_, reject) => {
    timer = setTimeout(() => reject(Object.assign(new Error(`${what} timed out after ${ms} ms`), { code: 'health_timeout' })), ms)
  })
  return Promise.race([promise, timeout]).finally(() => clearTimeout(timer))
}

function dropSyncState() {
  for (const name of readdirSync(stateDir)) {
    if (name.startsWith('kakaotalk-sync-state-')) rmSync(join(stateDir, name), { force: true })
  }
}

function onSessionEvent(evt) {
  if (closing) return
  switch (evt.type) {
    case 'connected':
      emit('connected', { user_id: String(evt.userId ?? '') })
      break
    case 'disconnected':
      log('LOCO session closed; reconnecting')
      emit('disconnected', {})
      void reconnectLoop()
      break
    case 'kicked':
      log('kicked:', evt.reason)
      emit('kicked', { reason: evt.reason ?? '' })
      shutdown(EXIT_KICKED)
      break
  }
}

// Re-opens the session after a close. acquireSession() logs in again with the saved
// sync state and emits 'connected' through onSessionEvent, which the bridge answers
// with a chat resync (catching up on anything missed while disconnected).
async function reconnectLoop() {
  if (reconnecting || closing || !client) return
  reconnecting = true
  let delay = RECONNECT_MIN_MS
  try {
    while (!closing) {
      await sleep(delay)
      if (closing) return
      if (client.isConnected()) return // an API call already reconnected
      try {
        await client.acquireSession()
        log('LOCO session restored')
        return
      } catch (err) {
        if (isAuthError(err)) {
          log('credentials rejected:', err?.message)
          emit('fatal', { code: err?.code ?? 'error', message: err?.message ?? String(err) })
          shutdown(EXIT_BAD_CREDENTIALS)
          return
        }
        delay = Math.min(delay * 2, RECONNECT_MAX_MS)
        log(`reconnect failed (${err?.code ?? 'error'}): ${err?.message ?? err}; next try in ${delay / 1000} s`)
      }
    }
  } finally {
    reconnecting = false
  }
}

// Pings are fire-and-forget inside agent-messenger, so a half-open socket (laptop
// woke up on another network) is only noticed when TCP gives up. A CHATINFO round
// trip every HEALTH_INTERVAL_MS catches it; on a transport failure the session is
// closed, which fires 'disconnected' and the reconnect loop.
async function healthCheck() {
  if (closing || reconnecting || !client || !client.isConnected() || !healthChatId) return
  try {
    await withTimeout(client.getChat(healthChatId), HEALTH_TIMEOUT_MS, 'health check')
  } catch (err) {
    if (!isTransportError(err)) {
      log('health check error (ignored):', err?.message ?? err)
      return
    }
    log('health check failed, dropping the session:', err?.message ?? err)
    try {
      if (client.isConnected()) (await client.acquireSession()).close()
    } catch {}
  }
}

function rememberHealthChat(chats) {
  if (!Array.isArray(chats) || chats.length === 0) return
  const memo = chats.find((c) => c?.type === 'MemoChat')
  healthChatId = (memo ?? chats[0])?.chat_id ?? healthChatId
}

async function start() {
  if (client) return plain(await client.getProfile())
  dropSyncState()
  const c = await new KakaoTalkClient().login()
  client = c
  unsubscribeSession = c.onSessionEvent(onSessionEvent)
  try {
    // Fail here (with agent-messenger's error code) rather than inside the listener,
    // which swallows login errors into an 'error' event.
    await c.acquireSession()
  } catch (err) {
    unsubscribeSession?.()
    unsubscribeSession = null
    client = null
    try { c.close() } catch {}
    throw err
  }
  listener = new KakaoTalkListener(c)
  listener.on('error', (err) => emit('error', { message: err?.message ?? String(err) }))
  listener.on('message', (evt) => emit('message', plain(evt)))
  listener.on('read', (evt) => emit('read', plain(evt)))
  listener.on('member_joined', (evt) => emit('member_joined', plain(evt)))
  listener.on('member_left', (evt) => emit('member_left', plain(evt)))
  await listener.start()
  if (!healthTimer) {
    healthTimer = setInterval(() => void healthCheck(), HEALTH_INTERVAL_MS)
    healthTimer.unref?.()
  }
  return plain(await c.getProfile())
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
  getChats: async (p) => {
    const chats = plain(await need().getChats({ all: p.all ?? true, resolveTitles: p.resolve_titles ?? false }))
    rememberHealthChat(chats)
    return chats
  },
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
  closing = true
  if (healthTimer) clearInterval(healthTimer)
  try { unsubscribeSession?.() } catch {}
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
