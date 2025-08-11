import { j, readSSE } from './utils.js'

describe('pipe', () => {
  it('send/recv over channel', async () => {
    const recv = readSSE('/pipe/recv/stream?channel=x', { max: 1, timeoutMs: 4000 })
    const send = await j('/pipe/send', { method: 'POST', body: JSON.stringify({ channel: 'x', object: { n: 42 } }) })
    expect(send.status).toBe(200)
    const events = await recv
    expect(events[0].object.n).toBe(42)
  })
})
