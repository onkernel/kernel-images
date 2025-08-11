import { j, readSSE } from './utils.js'

describe('metrics', () => {
  it('snapshot returns numbers', async () => {
    const { status, body } = await j('/metrics/snapshot')
    expect(status).toBe(200)
    expect(body).toHaveProperty('cpu_pct')
    expect(body).toHaveProperty('mem')
  })

  it('stream yields events', async () => {
    const events = await readSSE('/metrics/stream', { max: 2, timeoutMs: 4000 })
    expect(events.length).toBeGreaterThanOrEqual(1)
  })
})
