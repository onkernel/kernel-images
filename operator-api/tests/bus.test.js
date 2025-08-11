import { j, readSSE } from './utils.js'

describe('bus', () => {
  it('publish/subscribe via SSE', async () => {
    const eventsPromise = readSSE(`/bus/subscribe?channel=testch`, { max: 1, timeoutMs: 4000 })
    const pub = await j('/bus/publish', { method: 'POST', body: JSON.stringify({ channel: 'testch', type: 'note', payload: { msg: 'hello' } }) })
    expect(pub.status).toBe(200)
    const events = await eventsPromise
    expect(events.length).toBe(1)
    expect(events[0].payload.msg).toBe('hello')
  })
})
