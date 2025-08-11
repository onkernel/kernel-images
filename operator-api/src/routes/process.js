import 'dotenv/config'
import { Hono } from 'hono'
import { spawn } from 'node:child_process'
import { b64, fromB64 } from '../utils/base64.js'
import { uid } from '../utils/ids.js'
import { sseHeaders } from '../utils/sse.js'

const procs = new Map() // id -> {proc, started_at}

export const processRouter = new Hono()

function toEnv(obj) {
  const env = { ...process.env }
  for (const k of Object.keys(obj || {})) env[k] = String(obj[k])
  return env
}

processRouter.post('/process/exec', async (c) => {
  try {
    const body = await c.req.json()
    const { command, args = [], cwd, env = {}, as_root = false, timeout_sec = null, stream = false, as_user } = body
    if (!command) return c.json({ message: 'Missing command' }, 400)
    const child = spawn(command, args, { cwd: cwd || undefined, env: toEnv(env), shell: false })
    let stdout = Buffer.alloc(0)
    let stderr = Buffer.alloc(0)
    child.stdout?.on('data', (d) => (stdout = Buffer.concat([stdout, d])))
    child.stderr?.on('data', (d) => (stderr = Buffer.concat([stderr, d])))
    const code = await new Promise((resolve) => child.on('close', resolve))
    return c.json({ exit_code: code, stdout_b64: b64(stdout), stderr_b64: b64(stderr), duration_ms: 0 })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

processRouter.post('/process/spawn', async (c) => {
  try {
    const body = await c.req.json()
    const { command, args = [], cwd, env = {}, as_root = false, timeout_sec = null } = body
    if (!command) return c.json({ message: 'Missing command' }, 400)
    const process_id = uid()
    const child = spawn(command, args, { cwd: cwd || undefined, env: toEnv(env), shell: false })
    procs.set(process_id, { proc: child, started_at: new Date().toISOString(), pid: child.pid })
    return c.json({ process_id, pid: child.pid, started_at: new Date().toISOString() })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

processRouter.get('/process/:process_id/status', async (c) => {
  const id = c.req.param('process_id')
  const item = procs.get(id)
  if (!item) return c.json({ message: 'Not Found' }, 404)
  const state = item.proc.killed ? 'exited' : item.proc.exitCode == null ? 'running' : 'exited'
  return c.json({ state, exit_code: item.proc.exitCode ?? null, cpu_pct: 0, mem_bytes: 0 })
})

processRouter.get('/process/:process_id/stdout/stream', async (c) => {
  const id = c.req.param('process_id')
  const item = procs.get(id)
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

processRouter.post('/process/:process_id/stdin', async (c) => {
  const id = c.req.param('process_id')
  const item = procs.get(id)
  if (!item) return c.json({ message: 'Not Found' }, 404)
  const body = await c.req.json()
  const buf = Buffer.from(body.data_b64, 'base64')
  item.proc.stdin?.write(buf)
  return c.json({ written_bytes: buf.length })
})

processRouter.post('/process/:process_id/kill', async (c) => {
  const id = c.req.param('process_id')
  const item = procs.get(id)
  if (!item) return c.json({ message: 'Not Found' }, 404)
  const body = await c.req.json()
  const sigMap = { TERM: 'SIGTERM', KILL: 'SIGKILL', INT: 'SIGINT', HUP: 'SIGHUP' }
  item.proc.kill(sigMap[body.signal] || 'SIGTERM')
  return c.json({ ok: true })
})
