import { Hono } from 'hono'
import { uid } from '../utils/ids.js'
import { sseHeaders, sseFormat } from '../utils/sse.js'
import { EventEmitter } from 'node:events'

const sessions = new Map() // id -> emitter

export const browserRouter = new Hono()

browserRouter.post('/browser/har/start', async (c) => {
  const body = await c.req.json().catch(() => ({}))
  const har_session_id = uid()
  sessions.set(har_session_id, new EventEmitter())
  return c.json({ har_session_id })
})

browserRouter.get('/browser/har/:har_session_id/stream', async (c) => {
  const id = c.req.param('har_session_id')
  const em = sessions.get(id)
  if (!em) return c.json({ message: 'Not Found' }, 404)
  const stream = new ReadableStream({
    start(controller) {
      const h = (obj) => controller.enqueue(new TextEncoder().encode(sseFormat(obj)))
      em.on('har', h)
    }
  })
  return new Response(stream, { headers: sseHeaders() })
})

browserRouter.post('/browser/har/stop', async (c) => {
  const body = await c.req.json()
  const id = body?.har_session_id
  const em = sessions.get(id)
  if (!em) return c.json({ message: 'Not Found' }, 404)
  sessions.delete(id)
  return c.json({ ok: true })
})
