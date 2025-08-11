import { j, hasCmd, readSSE } from './utils.js'

const ffmpegPresent = hasCmd('ffmpeg')

describe('stream', () => {
  it.skipIf?.(!ffmpegPresent)('start metrics stream then stop', async () => {
    const start = await j('/stream/start', { method: 'POST', body: JSON.stringify({ rtmps_url: 'rtmps://example.com/live', stream_key: 'testkey', fps: 1, audio: { capture_system: false } }) })
    expect([200, 400]).toContain(start.status)
    if (start.status === 200) {
      const { stream_id } = start.body
      const ev = await readSSE(`/stream/${stream_id}/metrics/stream`, { max: 1, timeoutMs: 5000 })
      expect(Array.isArray(ev)).toBe(true)
      const stop = await j('/stream/stop', { method: 'POST', body: JSON.stringify({ stream_id }) })
      expect(stop.status).toBe(200)
    }
  })
})
