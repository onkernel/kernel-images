import { raw } from './utils.js'

describe('logs', () => {
  it('rejects bad request', async () => {
    const res = await raw('/logs/stream')
    expect(res.status).toBe(400)
  })
})
