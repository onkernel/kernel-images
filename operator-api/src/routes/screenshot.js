import 'dotenv/config'
import fs from 'node:fs'
import path from 'node:path'
import { Hono } from 'hono'
import { uid } from '../utils/ids.js'
import { b64 } from '../utils/base64.js'
import { execCapture } from '../utils/exec.js'
import { SCREENSHOTS_DIR } from '../utils/env.js'

const FFMPEG = process.env.FFMPEG_BIN || '/usr/bin/ffmpeg'
const DISPLAY = process.env.DISPLAY || ':0'
const WIDTH = Number(process.env.WIDTH || 1024)
const HEIGHT = Number(process.env.HEIGHT || 768)

if (DISPLAY == ':20') {
  console.warn(`DISPLAY from env: ${DISPLAY} [likely for debugging in a remote VM]`)
}

async function capture({ region, include_cursor, display = 0, format = 'png', quality }) {
  const id = uid()
  const ext = format === 'jpeg' ? 'jpg' : 'png'
  const outPath = path.join(SCREENSHOTS_DIR, `${id}.${ext}`)

  // Try grim (Wayland), fallback to ffmpeg x11grab
  let ok = false
  // grim region format: x y w h
  const grimRegion = region ? `${region.x},${region.y} ${region.width}x${region.height}` : null

  if (!ok) {
    const { code } = await execCapture('bash', ['-lc', `command -v grim >/dev/null 2>&1`])
    if (code === 0) {
      const args = []
      if (grimRegion) args.push('-g', grimRegion)
      args.push(outPath)
      const res = await execCapture('grim', args)
      ok = res.code === 0
    }
  }

  if (!ok) {
    // We can omit video_size as ffmpeg will detect the screen dimensions automatically
    const input = ['-f', 'x11grab', '-i', `${DISPLAY}.0`]
    const vf = region ? `crop=${region.width}:${region.height}:${region.x}:${region.y}` : 'null'
    const args = [...input, '-vframes', '1', '-vf', vf, outPath]
    const res = await execCapture(FFMPEG, ['-y', ...args])
    ok = res.code === 0
  }

  if (!fs.existsSync(outPath)) throw new Error('Capture failed')
  const bytes = fs.readFileSync(outPath)
  return { id, path: outPath, content_type: ext === 'jpg' ? 'image/jpeg' : 'image/png', bytes }
}

export const screenshotRouter = new Hono()

screenshotRouter.post('/screenshot/capture', async (c) => {
  try {
    const body = await c.req.json().catch(() => ({}))
    const res = await capture(body || {})
    return c.json({ screenshot_id: res.id, content_type: res.content_type, bytes_b64: b64(res.bytes) })
  } catch (e) {
    return c.json({ message: String(e.message || e) }, 500)
  }
})

screenshotRouter.get('/screenshot/:screenshot_id', async (c) => {
  const id = c.req.param('screenshot_id')
  const png = path.join(SCREENSHOTS_DIR, `${id}.png`)
  const jpg = path.join(SCREENSHOTS_DIR, `${id}.jpg`)
  let file = null
  if (fs.existsSync(png)) file = png
  else if (fs.existsSync(jpg)) file = jpg
  if (!file) return c.json({ message: 'Not Found' }, 404)
  const stream = fs.createReadStream(file)
  const ct = file.endsWith('.jpg') ? 'image/jpeg' : 'image/png'
  return new Response(stream, { headers: { 'Content-Type': ct } })
})
