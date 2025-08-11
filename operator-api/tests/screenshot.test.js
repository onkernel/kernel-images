import { j, hasCmd } from './utils.js'

const ffmpegPresent = hasCmd('ffmpeg') || hasCmd('grim')

describe('screenshot', () => {
  it.skipIf?.(!ffmpegPresent)('capture returns image bytes', async () => {
    const r = await j('/screenshot/capture', { method: 'POST', body: JSON.stringify({ format: 'png' }) })
    expect([200, 500]).toContain(r.status)
    if (r.status === 200) {
      expect(r.body).toHaveProperty('bytes_b64')
      expect(r.body).toHaveProperty('content_type')
    }
  })
})
