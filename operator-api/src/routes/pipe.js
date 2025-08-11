import { Hono } from 'hono'
import { EventEmitter } from 'node:events'
import { sseHeaders, sseFormat } from '../utils/sse.js'

const channels = new Map() // name -> emitter

function getEmitter(name) {
  if (!channels.has(name)) channels.set(name, new EventEmitter())
  return channels.get(name)
}

export const pipeRouter = new Hono()

pipeRouter.post('/pipe/send', async (c) => {
  const body = await c.req.json()
  const ch = getEmitter(body.channel || 'default')
  ch.emit('msg', { ts: new Date().toISOString(), object: body.object })
  return c.json({ enqueued: true })
})

pipeRouter.get('/pipe/recv/stream', async (c) => {
  const chName = c.req.query('channel') || 'default'
  const ch = getEmitter(chName)
  const stream = new ReadableStream({
    start(controller) {
      const h = (obj) => controller.enqueue(new TextEncoder().encode(sseFormat(obj)))
      ch.on('msg', h)
    }
  })
  return new Response(stream, { headers: sseHeaders() })
})
