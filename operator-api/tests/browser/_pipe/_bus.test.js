
import { describe, it, expect } from 'vitest'

describe('Browser/Bus/Pipe basic endpoints', () => {
it('starts browser HAR session', async () => {
const { app } = await import('../src/app.js')
const res = await app.request('/browser/har/start', { method: 'POST', body: JSON.stringify({}) })
expect(res.status).toBe(200)
const body = await res.json()
expect(body).toHaveProperty('har\_session\_id')
})

it('publishes on bus', async () => {
const { app } = await import('../src/app.js')
const res = await app.request('/bus/publish', { method: 'POST', body: JSON.stringify({ channel: 'default', type: 't', payload: {} }) })
expect(res.status).toBe(200)
const body = await res.json()
expect(body.delivered).toBe(true)
})

it('sends on pipe', async () => {
const { app } = await import('../src/app.js')
const res = await app.request('/pipe/send', { method: 'POST', body: JSON.stringify({ channel: 'default', object: { a: 1 } }) })
expect(res.status).toBe(200)
const body = await res.json()
expect(body.enqueued).toBe(true)
})
})

\================================================