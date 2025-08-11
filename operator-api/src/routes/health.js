import { Hono } from 'hono'
export const healthRouter = new Hono()
const start = Date.now()
healthRouter.get('/health', async (c) => {
  return c.json({ status: 'ok', uptime_sec: Math.round((Date.now() - start) / 1000) })
})
