import { Hono } from 'hono'
import { startStream, stopStream, metricsEmitter } from '../services/streamService.js'
import { sseHeaders, sseFormat } from '../utils/sse.js'

export const streamRouter = new Hono()

streamRouter.post('/stream/start', async (c) => {
  try {
    const body = await c.req.json()
    const { stream_id } = startStream(body)
    return c.json({ stream_id, status: 'starting', metrics_endpoint: `/stream/${stream_id}/metrics/stream` })
  } catch (e) {
    return c.json({ message: String(e.message || e) }, 400)
  }
})

streamRouter.post('/stream/stop', async (c) => {
  try {
    const body = await c.req.json()
    stopStream(body)
    return c.json({ stream_id: body.stream_id, status: 'stopped' })
  } catch (e) {
    return c.json({ message: String(e.message || e) }, 404)
  }
})

streamRouter.get('/stream/:stream_id/metrics/stream', async (c) => {
  const id = c.req.param('stream_id')
  const em = metricsEmitter(id)
  if (!em) return c.json({ message: 'Not Found' }, 404)
  const stream = new ReadableStream({
    start(controller) {
      const onData = (obj) => controller.enqueue(new TextEncoder().encode(sseFormat(obj)))
      em.on('metrics', onData)
    },
    cancel() {}
  })
  return new Response(stream, { headers: sseHeaders() })
})
