import { Hono } from 'hono'
import { execCapture } from '../utils/exec.js'
import chalk from 'chalk'

export const clipboardRouter = new Hono()

// Debug logging function
const debug = (message) => {
  if (process.env.DEBUG_LOGS) {
    console.log(chalk.cyan('[clipboard]'), message)
  }
}

async function wlGet() {
  debug('Attempting to get clipboard content using wl-paste')
  const txt = await execCapture('bash', ['-lc', 'wl-paste -t text 2>/dev/null || true'])
  if (txt.stdout?.length) return { type: 'text', text: txt.stdout.toString('utf8') }
  const png = await execCapture('bash', ['-lc', 'wl-paste -t image/png | base64 -w0 2>/dev/null || true'])
  if (png.stdout?.length) return { type: 'image', image_b64: png.stdout.toString('utf8'), image_mime: 'image/png' }
  return { type: 'text', text: '' }
}

async function wlSet({ type, text, image_b64, image_mime }) {
  debug(`Setting clipboard with wayland: type=${type}`)
  if (type === 'text') await execCapture('bash', ['-lc', `printf %s ${JSON.stringify(text || '')} | wl-copy`])
  else if (type === 'image' && image_b64) await execCapture('bash', ['-lc', `base64 -d | wl-copy -t ${image_mime || 'image/png'}`], { input: Buffer.from(image_b64, 'base64') })
}

async function xGet() {
  debug('Attempting to get clipboard content using xclip')
  const display = process.env.DISPLAY || ':20'
  const txt = await execCapture('bash', ['-lc', `DISPLAY=${display} xclip -selection clipboard -o 2>/dev/null || true`])
  return { type: 'text', text: txt.stdout.toString('utf8') }
}

async function xSet({ type, text }) {
  debug(`Setting clipboard with xclip: type=${type}`)
  const display = process.env.DISPLAY || ':20'
  if (type === 'text') await execCapture('bash', ['-lc', `printf %s ${JSON.stringify(text || '')} | DISPLAY=${display} xclip -selection clipboard`])
}

clipboardRouter.get('/clipboard', async (c) => {
  try {
    debug('GET /clipboard request received')
    // Try xclip first, fall back to wayland
    try {
      debug('Trying xclip first')
      return c.json(await xGet())
    } catch (error) {
      debug(`xclip failed: ${error.message}, trying wayland`)
      return c.json(await wlGet())
    }
  } catch (error) {
    debug(`Error in GET /clipboard: ${error.message}`)
    return c.json({ type: 'text', text: '' })
  }
})

clipboardRouter.post('/clipboard', async (c) => {
  try {
    debug('POST /clipboard request received')
    const body = await c.req.json()
    
    // Try xclip first, fall back to wayland
    try {
      debug('Trying to set clipboard with xclip')
      await xSet(body)
    } catch (error) {
      debug(`xclip set failed: ${error.message}, trying wayland`)
      await wlSet(body)
    }
    
    return c.json({ ok: true })
  } catch (e) {
    debug(`Error in POST /clipboard: ${e.message}`)
    return c.json({ message: String(e.message) }, 400)
  }
})

clipboardRouter.get('/clipboard/stream', async (c) => {
  debug('GET /clipboard/stream request received')
  // simple poller
  let lastText = ''
  const stream = new ReadableStream({
    async start(controller) {
      const enc = new TextEncoder()
      const loop = async () => {
        try {
          let res = { type: 'text', text: '' }
          
          // Try xclip first, fall back to wayland
          try {
            res = await xGet()
          } catch {
            try {
              res = await wlGet()
            } catch (error) {
              debug(`Both clipboard methods failed: ${error.message}`)
            }
          }
          
          const curr = res.type === 'text' ? res.text : res.image_b64?.slice(0, 16) || ''
          if (curr !== lastText) {
            lastText = curr
            debug(`Clipboard content changed, sending update: ${res.type}`)
            controller.enqueue(enc.encode(`event: data\ndata: ${JSON.stringify({ ts: new Date().toISOString(), type: res.type, preview: res.type === 'text' ? (res.text || '').slice(0, 100) : 'image...' })}\n\n`))
          }
        } catch (error) {
          debug(`Error in clipboard stream: ${error.message}`)
        }
        setTimeout(loop, 1000)
      }
      loop()
    },
    cancel() {
      debug('Clipboard stream cancelled')
    }
  })
  return new Response(stream, { headers: { ...{ 'Cache-Control': 'no-cache' }, ...{ 'Content-Type': 'text/event-stream' }, 'X-SSE-Content-Type': 'application/json' } })
})
