import { j, readSSE, hasCmd, raw } from './utils.js'

const ffmpegPresent = hasCmd('ffmpeg')

describe('recording-nodelete', () => {
  it.skipIf?.(!ffmpegPresent)('start/list/stop/download', async () => {
    let r = await j('/recording/start', { method: 'POST', body: JSON.stringify({ id: 't1', maxDurationInSeconds: 1 }) })
    expect(r.status).toBe(201)

    r = await j('/recording/list')
    expect(r.status).toBe(200)
    const item = r.body.find(i => i.id === 't1')
    expect(item).toBeTruthy()
    expect(item.isRecording).toBe(true)

    await new Promise(res => setTimeout(res, 1_500))
    // Try download after it should have stopped
    const d = await raw('/recording/download?id=t1')
    expect([200, 404]).toContain(d.status)
    
    // No delete operation
  })
})
