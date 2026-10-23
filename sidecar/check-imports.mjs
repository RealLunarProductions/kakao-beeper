// Verifies the sidecar's runtime imports resolve (run with node from any cwd).
const { KakaoTalkClient } = await import('agent-messenger/kakaotalk')
const { Long } = await import('bson')
console.log('imports ok:', typeof KakaoTalkClient.prototype.acquireSession, Long.fromString('100000000000001').toString())
