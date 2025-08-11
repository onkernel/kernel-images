import { Hono } from 'hono'
import { uid } from '../utils/ids.js'
import { execCapture } from '../utils/exec.js'
import 'dotenv/config'

const macros = new Map() // id -> {name, steps}
const display = process.env.DISPLAY || ':0'
const XDOTOOL = process.env.XDOTOOL_BIN || '/usr/bin/xdotool'

export const macrosRouter = new Hono()

async function runXdotool(args) {
  const res = await execCapture(XDOTOOL, args, { env: { ...process.env, DISPLAY: display } })
  if (res.code !== 0) throw new Error(res.stderr.toString('utf8') || 'xdotool error')
  return res
}

macrosRouter.post('/macros/create', async (c) => {
  const body = await c.req.json()
  if (!body?.name || !Array.isArray(body.steps)) return c.json({ message: 'Bad Request' }, 400)
  const id = uid()
  macros.set(id, { macro_id: id, name: body.name, steps: body.steps })
  return c.json({ macro_id: id })
})

macrosRouter.post('/macros/run', async (c) => {
  const body = await c.req.json()
  const item = [...macros.values()].find((m) => m.macro_id === body.macro_id)
  if (!item) return c.json({ message: 'Not Found' }, 404)
  const run_id = uid()
    ; (async () => {
      for (const step of item.steps) {
        if (step.action === 'keyboard.type' && step.text) await runXdotool(['type', '--clearmodifiers', '--', step.text])
        else if (step.action === 'keyboard.key' && step.key) await runXdotool(['key', step.key])
        else if (step.action === 'sleep' && step.ms) await runXdotool(['sleep', String(step.ms / 1000)])
      }
    })()
  return c.json({ started: true, run_id })
})

macrosRouter.get('/macros/list', async (c) => {
  return c.json({ items: [...macros.values()].map((m) => ({ macro_id: m.macro_id, name: m.name, steps_count: m.steps.length })) })
})

macrosRouter.delete('/macros/:macro_id', async (c) => {
  const id = c.req.param('macro_id')
  if (!macros.has(id)) return c.json({ message: 'Not Found' }, 404)
  macros.delete(id)
  return c.json({ ok: true })
})