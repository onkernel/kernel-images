import fs from 'node:fs'
import path from 'node:path'
import { Hono } from 'hono'
import { DATA_DIR } from '../utils/env.js'
import { b64 } from '../utils/base64.js'
import { sseHeaders, sseFormat } from '../utils/sse.js'
import chokidar from 'chokidar'
import { uid } from '../utils/ids.js'

export const fsRouter = new Hono()

function ok(c) { return c.json({ ok: true }) }

fsRouter.get('/fs/read_file', async (c) => {
  const p = c.req.query('path')
  if (!p || !p.startsWith('/')) return c.json({ message: 'Invalid path' }, 400)
  if (!fs.existsSync(p)) return c.json({ message: 'Not Found' }, 404)
  const stream = fs.createReadStream(p)
  return new Response(stream, { status: 200, headers: { 'Content-Type': 'application/octet-stream' } })
})

fsRouter.put('/fs/write_file', async (c) => {
  const url = new URL(c.req.url)
  const p = url.searchParams.get('path')
  const mode = url.searchParams.get('mode') || '0644'
  if (!p || !p.startsWith('/')) return c.json({ message: 'Invalid path' }, 400)
  const buf = Buffer.from(await c.req.arrayBuffer())
  fs.mkdirSync(path.dirname(p), { recursive: true })
  fs.writeFileSync(p, buf, { mode: parseInt(mode, 8) })
  return c.json({ ok: true }, 201)
})

fsRouter.get('/fs/list_files', async (c) => {
  const p = c.req.query('path')
  if (!p || !p.startsWith('/')) return c.json({ message: 'Invalid path' }, 400)
  if (!fs.existsSync(p)) return c.json({ message: 'Not Found' }, 404)
  const names = fs.readdirSync(p)
  const items = names.map((name) => {
    const fp = path.join(p, name)
    const st = fs.statSync(fp)
    return {
      name, path: fp, size_bytes: st.isDirectory() ? 0 : st.size,
      is_dir: st.isDirectory(),
      mod_time: st.mtime.toISOString(),
      mode: (st.mode & 0o7777).toString(8)
    }
  })
  return c.json(items)
})

fsRouter.put('/fs/create_directory', async (c) => {
  const body = await c.req.json()
  const p = body?.path
  const mode = body?.mode ? parseInt(body.mode, 8) : 0o755
  if (!p || !p.startsWith('/')) return c.json({ message: 'Invalid path' }, 400)
  fs.mkdirSync(p, { recursive: true, mode })
  return c.json({ ok: true }, 201)
})

fsRouter.put('/fs/delete_file', async (c) => {
  const body = await c.req.json()
  const p = body?.path
  if (!p || !p.startsWith('/')) return c.json({ message: 'Invalid path' }, 400)
  if (!fs.existsSync(p)) return c.json({ message: 'Not Found' }, 404)
  const st = fs.statSync(p)
  if (st.isDirectory()) return c.json({ message: 'Is directory' }, 400)
  fs.rmSync(p, { force: true })
  return ok(c)
})

fsRouter.put('/fs/delete_directory', async (c) => {
  const body = await c.req.json()
  const p = body?.path
  if (!p || !p.startsWith('/')) return c.json({ message: 'Invalid path' }, 400)
  fs.rmSync(p, { recursive: true, force: true })
  return ok(c)
})

fsRouter.put('/fs/set_file_permissions', async (c) => {
  const body = await c.req.json()
  const p = body?.path
  if (!p || !p.startsWith('/')) return c.json({ message: 'Invalid path' }, 400)
  if (!fs.existsSync(p)) return c.json({ message: 'Not Found' }, 404)
  if (body.mode) fs.chmodSync(p, parseInt(body.mode, 8))
  if (body.owner || body.group) {
    try {
      const uid = body.owner ? Number.isNaN(Number(body.owner)) ? process.getuid?.() : Number(body.owner) : undefined
      const gid = body.group ? Number.isNaN(Number(body.group)) ? process.getgid?.() : Number(body.group) : undefined
      if (uid !== undefined || gid !== undefined) fs.chownSync(p, uid ?? fs.statSync(p).uid, gid ?? fs.statSync(p).gid)
    } catch {}
  }
  return ok(c)
})

fsRouter.get('/fs/file_info', async (c) => {
  const p = c.req.query('path')
  if (!p || !p.startsWith('/')) return c.json({ message: 'Invalid path' }, 400)
  if (!fs.existsSync(p)) return c.json({ message: 'Not Found' }, 404)
  const st = fs.statSync(p)
  return c.json({
    name: path.basename(p),
    path: p,
    size_bytes: st.isDirectory() ? 0 : st.size,
    is_dir: st.isDirectory(),
    mod_time: st.mtime.toISOString(),
    mode: (st.isDirectory() ? 'd' : '-') + (st.mode & 0o777).toString(8)
  })
})

fsRouter.put('/fs/move', async (c) => {
  const body = await c.req.json()
  if (!body?.src_path || !body?.dest_path) return c.json({ message: 'Missing paths' }, 400)
  fs.mkdirSync(path.dirname(body.dest_path), { recursive: true })
  fs.renameSync(body.src_path, body.dest_path)
  return ok(c)
})

const watches = new Map() // id -> {watcher, path}
fsRouter.post('/fs/watch', async (c) => {
  const body = await c.req.json()
  if (!body?.path) return c.json({ message: 'Missing path' }, 400)
  const id = uid()
  const watcher = chokidar.watch(body.path, { ignoreInitial: true, persistent: true, depth: body.recursive ? undefined : 0 })
  watches.set(id, { watcher, path: body.path })
  return c.json({ watch_id: id }, 201)
})

fsRouter.get('/fs/watch/:watch_id/events', async (c) => {
  const id = c.req.param('watch_id')
  const item = watches.get(id)
  if (!item) return c.json({ message: 'Not Found' }, 404)
  const { watcher } = item
  const stream = new ReadableStream({
    start(controller) {
      const send = (type, p, is_dir) => controller.enqueue(new TextEncoder().encode(`event: data\ndata: ${JSON.stringify({ type, name: p.split('/').pop(), path: p, is_dir })}\n\n`))
      watcher.on('add', (p) => send('CREATE', p, false))
      watcher.on('addDir', (p) => send('CREATE', p, true))
      watcher.on('change', (p) => send('WRITE', p, false))
      watcher.on('unlink', (p) => send('DELETE', p, false))
      watcher.on('unlinkDir', (p) => send('DELETE', p, true))
    },
    cancel() {}
  })
  return new Response(stream, { headers: { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache', Connection: 'keep-alive', 'X-SSE-Content-Type': 'application/json' } })
})

fsRouter.delete('/fs/watch/:watch_id', async (c) => {
  const id = c.req.param('watch_id')
  const item = watches.get(id)
  if (!item) return c.json({ message: 'Not Found' }, 404)
  await item.watcher.close()
  watches.delete(id)
  return new Response(null, { status: 204 })
})

fsRouter.post('/fs/upload', async (c) => {
  const form = await c.req.parseBody()
  const pathDest = form['path']
  const file = form['file']
  if (!pathDest || !file) return c.json({ message: 'Missing fields' }, 400)
  const buf = Buffer.from(await file.arrayBuffer())
  fs.mkdirSync(path.dirname(pathDest), { recursive: true })
  fs.writeFileSync(pathDest, buf)
  return c.json({ path: pathDest, size_bytes: buf.length })
})

fsRouter.get('/fs/download', async (c) => {
  const p = c.req.query('path')
  if (!p || !fs.existsSync(p)) return c.json({ message: 'Not Found' }, 404)
  const stream = fs.createReadStream(p)
  return new Response(stream, { headers: { 'Content-Type': 'application/octet-stream' } })
})

fsRouter.get('/fs/tail/stream', async (c) => {
  const p = c.req.query('path')
  if (!p || !fs.existsSync(p)) return c.json({ message: 'Not Found' }, 404)
  const { spawn } = await import('node:child_process')
  const proc = spawn('tail', ['-n', '0', '-F', p])
  const stream = new ReadableStream({
    start(controller) {
      proc.stdout.on('data', (d) => {
        const lines = d.toString('utf8').split('\n').filter(Boolean)
        for (const line of lines) controller.enqueue(new TextEncoder().encode(`event: data\ndata: ${JSON.stringify({ line, ts: new Date().toISOString() })}\n\n`))
      })
      proc.on('close', () => controller.close())
    },
    cancel() {
      proc.kill('SIGINT')
    }
  })
  return new Response(stream, { headers: sseHeaders() })
})
