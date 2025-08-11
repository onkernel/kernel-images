import { j, readSSE } from './utils.js'

describe('browser HAR sessions', () => {
  it('start/stream/stop', async () => {
    const start = await j('/browser/har/start', { method: 'POST', body: JSON.stringify({}) })
    expect(start.status).toBe(200)
    const { har_session_id } = start.body
    // const res = await fetch(`${globalThis.__TEST_BASE_URL__}/browser/har/${har_session_id}/stream`)
    const res = await fetch(`/browser/har/${har_session_id}/stream`)
    expect(res.status).toBe(200)
    const reader = res.body.getReader()
    await reader.cancel()
    const stop = await j('/browser/har/stop', { method: 'POST', body: JSON.stringify({ har_session_id }) })
    expect(stop.status).toBe(200)
  })
})
