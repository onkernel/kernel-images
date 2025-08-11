import { j } from './utils.js'

describe('os', () => {
  it('GET /os/locale returns locale info', async () => {
    const { status, body } = await j('/os/locale')
    expect(status).toBe(200)
    expect(body).toHaveProperty('locale')
    expect(body).toHaveProperty('keyboard_layout')
    expect(body).toHaveProperty('timezone')
  })

  it('POST /os/locale updates env', async () => {
    const { status, body } = await j('/os/locale', {
      method: 'POST',
      body: JSON.stringify({ locale: 'en_US.UTF-8', timezone: 'UTC', keyboard_layout: 'us' })
    })
    expect(status).toBe(200)
    expect(body.updated).toBe(true)
  })
})
