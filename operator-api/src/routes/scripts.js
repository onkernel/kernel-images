import 'dotenv/config'
import fs from 'node:fs'
import path from 'node:path'
import { Hono } from 'hono'
import { SCRIPTS_DIR } from '../utils/env.js'
import { uid } from '../utils/ids.js'
import { spawn } from 'node:child_process'
import { sseHeaders } from '../utils/sse.js'
import { b64 } from '../utils/base64.js'

const runs = new Map() // run_id -> {proc}

export const scriptsRouter = new Hono()

scriptsRouter.post('/scripts/upload', async (c) => {
  const form = await c.req.parseBody()
  const destPath = form['path']
  const file = form['file']
  const executable = String(form['executable'] || 'true') === 'true'
  if (!destPath || !file) return c.json({ message: 'Bad Request' }, 400)
  const abs = destPath.startsWith('/') ? destPath : path.join(SCRIPTS_DIR, destPath)
  fs.mkdirSync(path.dirname(abs), { recursive: true })
  const buf = Buffer.from(await file.arrayBuffer())
  fs.writeFileSync(abs, buf)
  if (executable) fs.chmodSync(abs, 0o755)
  return c.json({ path: abs, size_bytes: buf.length })
})

scriptsRouter.post('/scripts/run', async (c) => {
  const body = await c.req.json()
  const { path: scriptPath, args = [], cwd, env = {}, as_user = null, as_root = false, mode = 'sync', stream = true } = body
  if (!scriptPath || !fs.existsSync(scriptPath)) return c.json({ message: 'Not Found' }, 404)
  if (mode === 'async') {
    const run_id = uid()
    const proc = spawn(scriptPath, args, { cwd: cwd || path.dirname(scriptPath), env: { ...process.env, ...env }, shell: false })
    runs.set(run_id, { proc })
    return c.json({ run_id })
  } else {
    const proc = spawn(scriptPath, args, { cwd: cwd || path.dirname(scriptPath), env: { ...process.env, ...env }, shell: false })
    let stdout = Buffer.alloc(0)
    let stderr = Buffer.alloc(0)
    proc.stdout.on('data', (d) => (stdout = Buffer.concat([stdout, d])))
    proc.stderr.on('data', (d) => (stderr = Buffer.concat([stderr, d])))
    const code = await new Promise((res) => proc.on('close', res))
    return c.json({ exit_code: code, stdout_b64: b64(stdout), stderr_b64: b64(stderr), duration_ms: 0 })
  }
})

scriptsRouter.get('/scripts/run/:run_id/logs/stream', async (c) => {
  const run_id = c.req.param('run_id')
  const item = runs.get(run_id)
  if (!item) return c.json({ message: 'Not Found' }, 404)
  const stream = new ReadableStream({
    start(controller) {
      const enc = new TextEncoder()
      const send = (stream, data) => controller.enqueue(enc.encode(`event: data\ndata: ${JSON.stringify({ stream, data_b64: b64(data) })}\n\n`))
      item.proc.stdout?.on('data', (d) => send('stdout', d))
      item.proc.stderr?.on('data', (d) => send('stderr', d))
      item.proc.on('close', (code) => controller.enqueue(enc.encode(`event: data\ndata: ${JSON.stringify({ event: 'exit', exit_code: code })}\n\n`)))
    },
    cancel() {}
  })
  return new Response(stream, { headers: sseHeaders() })
})

scriptsRouter.get('/scripts/list', async (c) => {
  const items = []
  function walk(dir) {
    for (const name of fs.readdirSync(dir)) {
      const p = path.join(dir, name)
      const st = fs.statSync(p)
      if (st.isDirectory()) walk(p)
      else items.push({ path: p, size_bytes: st.size, updated_at: st.mtime.toISOString() })
    }
  }
  walk(SCRIPTS_DIR)
  return c.json({ items })
})

scriptsRouter.delete('/scripts/delete', async (c) => {
  const body = await c.req.json()
  const p = body?.path
  if (!p || !fs.existsSync(p)) return c.json({ message: 'Not Found' }, 404)
  fs.rmSync(p, { force: true })
  return c.json({ ok: true })
})
