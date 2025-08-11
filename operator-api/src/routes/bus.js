import { Hono } from 'hono'
import { EventEmitter } from 'node:events'
import { sseHeaders, sseFormat } from '../utils/sse.js'

const bus = new EventEmitter()

export const busRouter = new Hono()

busRouter.post('/bus/publish', async (c) => {
  const body = await c.req.json()
  bus.emit(body.channel || 'default', { ts: new Date().toISOString(), type: body.type, payload: body.payload })
  return c.json({ delivered: true })
})

busRouter.get('/bus/subscribe', async (c) => {
  const channel = c.req.query('channel')
  if (!channel) return c.json({ message: 'Missing channel' }, 400)
  const stream = new ReadableStream({
    start(controller) {
      const h = (obj) => controller.enqueue(new TextEncoder().encode(sseFormat(obj)))
      bus.on(channel, h)
    },
    cancel() {}
  })
  return new Response(stream, { headers: sseHeaders() })
})
