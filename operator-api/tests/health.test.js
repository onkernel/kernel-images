import { j } from './utils.js'

describe('health', () => {
  it('GET /health returns ok', async () => {
    const { status, body } = await j('/health')
    expect(status).toBe(200)
    expect(body.status).toBe('ok')
    expect(typeof body.uptime_sec).toBe('number')
  })
})
