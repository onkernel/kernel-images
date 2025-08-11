import 'dotenv/config'
import { Hono } from 'hono'

export const osRouter = new Hono()

osRouter.get('/os/locale', async (c) => {
  const locale = process.env.LANG || 'en_US.UTF-8'
  const keyboard_layout = process.env.XKB_DEFAULT_LAYOUT || 'us'
  const timezone = process.env.TZ || 'UTC'
  return c.json({ locale, keyboard_layout, timezone })
})

osRouter.post('/os/locale', async (c) => {
  const body = await c.req.json()
  const locale = body?.locale || process.env.LANG
  const keyboard_layout = body?.keyboard_layout || 'us'
  const timezone = body?.timezone || 'UTC'
  process.env.LANG = locale
  process.env.TZ = timezone
  return c.json({ updated: true, requires_restart: false })
})
