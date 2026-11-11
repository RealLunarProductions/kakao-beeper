// Verifies the sidecar's runtime imports resolve (run with node from any cwd),
// including the agent-messenger internals the media upload fix replaces.
import { createRequire } from 'node:module'
import { dirname, join } from 'node:path'
import { pathToFileURL } from 'node:url'

const { KakaoTalkClient } = await import('agent-messenger/kakaotalk')
const { Long } = await import('bson')
const require = createRequire(import.meta.url)
const protocol = join(dirname(require.resolve('agent-messenger/package.json')), 'dist/src/platforms/kakaotalk/protocol')
const { LocoConnection } = await import(pathToFileURL(join(protocol, 'connection.js')).href)
const { getLocoDeviceConfig, MCCMNC } = await import(pathToFileURL(join(protocol, 'config.js')).href)
const proto = KakaoTalkClient.prototype
const missing = Object.entries({
  acquireSession: proto.acquireSession,
  sendMediaViaLoco: proto.sendMediaViaLoco,
  executeWithReconnect: proto.executeWithReconnect,
  LocoConnection,
  'LocoConnection.writeRaw': LocoConnection?.prototype.writeRaw,
  getLocoDeviceConfig,
}).filter(([, v]) => typeof v !== 'function').map(([k]) => k)
if (missing.length || typeof MCCMNC !== 'string') {
  console.error('agent-messenger internals missing:', missing.join(', ') || 'MCCMNC')
  process.exit(1)
}
console.log('imports ok:', Long.fromString('100000000000001').toString())
