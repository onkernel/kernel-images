import { Hono } from 'hono'
// import { startSocks, stopSocks } from '../services/socksService.js'
import { applyRules, deleteRuleSet, harStreamEmitter } from '../services/interceptService.js'
import { addForward, removeForward } from '../services/forwardService.js'
import { sseHeaders, sseFormat } from '../utils/sse.js'

export const networkRouter = new Hono()

console.warn(`[operator-api/src/routes/network.js] : disabled socks5 endpoints + removed socks5 package for now (build crashes kernel container)`)

/*
networkRouter.post('/network/proxy/socks5/start', async (c) => {
  try {
    const body = await c.req.json()
    const { proxy_id, url } = startSocks(body)
    return c.json({ proxy_id, url })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

networkRouter.post('/network/proxy/socks5/stop', async (c) => {
  try {
    const body = await c.req.json()
    stopSocks(body)
    return c.json({ ok: true })
  } catch (e) {
    return c.json({ message: String(e.message) }, 404)
  }
})
*/

networkRouter.post('/network/intercept/rules', async (c) => {
  try {
    const body = await c.req.json()
    const res = applyRules(body)
    return c.json(res)
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

networkRouter.delete('/network/intercept/rules/:rule_set_id', async (c) => {
  try {
    deleteRuleSet({ rule_set_id: c.req.param('rule_set_id') })
    return c.json({ ok: true })
  } catch (e) {
    return c.json({ message: String(e.message) }, 404)
  }
})

networkRouter.get('/network/har/stream', async (c) => {
  const em = harStreamEmitter()
  const stream = new ReadableStream({
    start(controller) {
      const handler = (obj) => controller.enqueue(new TextEncoder().encode(sseFormat(obj)))
      em.on('har', handler)
    },
    cancel() {}
  })
  return new Response(stream, { headers: sseHeaders() })
})

networkRouter.post('/network/forward', async (c) => {
  try {
    const body = await c.req.json()
    const res = addForward(body)
    return c.json(res)
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

networkRouter.delete('/network/forward/:forward_id', async (c) => {
  try {
    const id = c.req.param('forward_id')
    removeForward({ forward_id: id })
    return c.json({ ok: true })
  } catch (e) {
    return c.json({ message: String(e.message) }, 404)
  }
})
