// src/routes/input.js
import 'dotenv/config'
import { Hono } from 'hono'
import { execCapture } from '../utils/exec.js'

export const inputRouter = new Hono()

const display = process.env.DISPLAY || ':0'
const XDOTOOL = process.env.XDOTOOL_BIN || '/usr/bin/xdotool'

// ---------- helpers ----------
function asString(v, fallback = '') {
  if (v === undefined || v === null) return fallback
  return String(v)
}



async function runXdotool(args) {
  const res = await execCapture(XDOTOOL, args, { env: { ...process.env, DISPLAY: display } })
  if (res.code !== 0) throw new Error(res.stderr.toString('utf8') || 'xdotool error')
  return res
}

function parseFirstId(stdout) {
  return stdout.toString('utf8').split('\n').filter(Boolean)[0]
}

function matchToSearchArgs(match) {
  const args = ['search']
  if (match && match.only_visible) args.push('--onlyvisible')
  if (match && (match.title_contains || match.name)) {
    args.push('--name', match.title_contains ?? match.name)
  }
  if (match && match.class) args.push('--class', match.class)
  if (match && match.pid) args.push('--pid', String(match.pid))
  if (args.length === 1) args.push('--onlyvisible', '.')
  return args
}

async function findWindowId(match) {
  const found = await execCapture('xdotool', matchToSearchArgs(match))
  const wid = parseFirstId(found.stdout)
  return wid || undefined
}


async function findWindowIdsMultiple(match) {
  const found = await execCapture('xdotool', matchToSearchArgs(match))
  const stdout = found.stdout.toString('utf8')
  const windowIds = stdout.split('\n').filter(Boolean)
  return windowIds.length > 0 ? windowIds : []
}


function buttonNumFromName(name) {
  if (typeof name === 'number') return String(name)
  const map = { left: 1, middle: 2, right: 3, back: 8, forward: 9 }
  return String(map[String(name)] ?? 1)
}

// ---------- mouse ----------
inputRouter.post('/input/mouse/move', async (c) => {
  try {
    const { x, y } = await c.req.json()
    if (typeof x !== 'number' || typeof y !== 'number') return c.json({ message: 'Invalid coords' }, 400)
    await runXdotool(['mousemove', String(x), String(y)])
    return c.json({ ok: true })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.post('/input/mouse/move_relative', async (c) => {
  try {
    const { dx = 0, dy = 0 } = await c.req.json()
    await runXdotool(['mousemove_relative', '--', String(dx), String(dy)])
    return c.json({ ok: true })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.post('/input/mouse/click', async (c) => {
  try {
    const body = await c.req.json()
    const button = buttonNumFromName(body.button)
    const count = body.count ?? body.num_clicks ?? 1
    await runXdotool(['click', '--repeat', String(count), button])
    return c.json({ ok: true })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.post('/input/mouse/down', async (c) => {
  try {
    const { button } = await c.req.json()
    await runXdotool(['mousedown', buttonNumFromName(button)])
    return c.json({ ok: true })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.post('/input/mouse/up', async (c) => {
  try {
    const { button } = await c.req.json()
    await runXdotool(['mouseup', buttonNumFromName(button)])
    return c.json({ ok: true })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.post('/input/mouse/scroll', async (c) => {
  try {
    const { dx = 0, dy = -120 } = await c.req.json()
    const verticalClicks = Math.max(1, Math.round(Math.abs(dy) / 120))
    const horizontalClicks = Math.max(0, Math.round(Math.abs(dx) / 120))
    const vButton = dy < 0 ? '5' : '4'
    const hButton = dx > 0 ? '7' : '6'
    for (let i = 0; i < verticalClicks; i++) await runXdotool(['click', vButton])
    for (let i = 0; i < horizontalClicks; i++) await runXdotool(['click', hButton])
    return c.json({ ok: true })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.get('/input/mouse/location', async (c) => {
  try {
    const res = await runXdotool(['getmouselocation', '--shell'])
    const out = res.stdout.toString('utf8').trim().split('\n')
    const kv = {}
    for (const line of out) {
      const [k, v] = line.split('=')
      kv[k] = v
    }
    return c.json({
      x: Number(kv['X']),
      y: Number(kv['Y']),
      screen: Number(kv['SCREEN']),
      window: asString(kv['WINDOW']),
    })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

// ---------- keyboard ----------
inputRouter.post('/input/keyboard/type', async (c) => {
  try {
    const { text, wpm = 300, enter = false } = await c.req.json()
    if (typeof text !== 'string') return c.json({ message: 'Missing text' }, 400)
    const delay = Math.max(1, Math.round(60000 / (wpm * 5)))
    await runXdotool(['type', '--delay', String(delay), '--clearmodifiers', '--', text])
    if (enter) await runXdotool(['key', 'Return'])
    return c.json({ ok: true })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.post('/input/keyboard/key', async (c) => {
  try {
    const { keys } = await c.req.json()
    const seq = Array.isArray(keys) ? keys : [asString(keys)]
    if (!seq.length || !seq[0]) return c.json({ message: 'Missing keys' }, 400)
    await runXdotool(['key', ...seq])
    return c.json({ ok: true })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.post('/input/keyboard/key_down', async (c) => {
  try {
    const { key } = await c.req.json()
    await runXdotool(['keydown', key])
    return c.json({ ok: true })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.post('/input/keyboard/key_up', async (c) => {
  try {
    const { key } = await c.req.json()
    await runXdotool(['keyup', key])
    return c.json({ ok: true })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

// ---------- windows ----------
inputRouter.post('/input/window/activate', async (c) => {
  try {
    const { match } = await c.req.json()
    const wid = await findWindowId(match)
    if (!wid) return c.json({ activated: false })
    await runXdotool(['windowactivate', wid])
    return c.json({ activated: true, wid })
  } catch {
    return c.json({ activated: false })
  }
})

inputRouter.post('/input/window/focus', async (c) => {
  try {
    const { match } = await c.req.json()
    const wid = await findWindowId(match)
    if (!wid) return c.json({ focused: false })
    await runXdotool(['windowfocus', wid])
    return c.json({ focused: true, wid })
  } catch {
    return c.json({ focused: false })
  }
})

inputRouter.post('/input/window/move_resize', async (c) => {
  try {
    const { match, x, y, width, height } = await c.req.json()
    const wid = await findWindowId(match)
    if (!wid) return c.json({ message: 'Not Found' }, 404)
    if (x !== undefined && y !== undefined) await runXdotool(['windowmove', wid, String(x), String(y)])
    if (width !== undefined && height !== undefined) await runXdotool(['windowsize', wid, String(width), String(height)])
    return c.json({ ok: true, wid })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.post('/input/window/raise', async (c) => {
  try {
    const { match } = await c.req.json()
    const wid = await findWindowId(match)
    if (!wid) return c.json({ message: 'Not Found' }, 404)
    await runXdotool(['windowraise', wid])
    return c.json({ ok: true, wid })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.post('/input/window/minimize', async (c) => {
  try {
    const { match } = await c.req.json()
    const wid = await findWindowId(match)
    if (!wid) return c.json({ message: 'Not Found' }, 404)
    await runXdotool(['windowminimize', wid])
    return c.json({ ok: true, wid })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.post('/input/window/map', async (c) => {
  try {
    const { match } = await c.req.json()
    const wid = await findWindowId(match)
    if (!wid) return c.json({ message: 'Not Found' }, 404)
    await runXdotool(['windowmap', wid])
    return c.json({ ok: true, wid })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.post('/input/window/unmap', async (c) => {
  try {
    const { match } = await c.req.json()
    const wid = await findWindowId(match)
    if (!wid) return c.json({ message: 'Not Found' }, 404)
    await runXdotool(['windowunmap', wid])
    return c.json({ ok: true, wid })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.post('/input/window/close', async (c) => {
  try {
    const { match } = await c.req.json()
    // Note: Using findWindowIdsMultiple instead of findWindowId to close multiple windows
    const windowIds = await findWindowIdsMultiple(match)
    if (windowIds.length === 0) return c.json({ message: 'Not Found' }, 404)
    
    for (const wid of windowIds) {
      await runXdotool(['windowclose', wid])
    }
    
    return c.json({ ok: true, windowIds })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

// Original version that only closes a single window
// inputRouter.post('/input/window/close', async (c) => {
//   try {
//     const { match } = await c.req.json()
//     const wid = await findWindowId(match)
//     if (!wid) return c.json({ message: 'Not Found' }, 404)
//     await runXdotool(['windowclose', wid])
//     return c.json({ ok: true, wid })
//   } catch (e) {
//     return c.json({ message: String(e.message) }, 400)
//   }
// })

inputRouter.post('/input/window/kill', async (c) => {
  try {
    const { match } = await c.req.json()
    const wid = await findWindowId(match)
    if (!wid) return c.json({ message: 'Not Found' }, 404)
    await runXdotool(['windowkill', wid])
    return c.json({ ok: true, wid })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.get('/input/window/active', async (c) => {
  try {
    const res = await runXdotool(['getactivewindow'])
    return c.json({ wid: parseFirstId(res.stdout) })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.get('/input/window/focused', async (c) => {
  try {
    const res = await runXdotool(['getwindowfocus'])
    return c.json({ wid: parseFirstId(res.stdout) })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.post('/input/window/name', async (c) => {
  try {
    const { wid } = await c.req.json()
    if (!wid) return c.json({ message: 'Missing wid' }, 400)
    const res = await runXdotool(['getwindowname', String(wid)])
    return c.json({ wid, name: res.stdout.toString('utf8').trim() })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.post('/input/window/pid', async (c) => {
  try {
    const { wid } = await c.req.json()
    if (!wid) return c.json({ message: 'Missing wid' }, 400)
    const res = await runXdotool(['getwindowpid', String(wid)])
    return c.json({ wid, pid: Number(res.stdout.toString('utf8').trim()) })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.post('/input/window/geometry', async (c) => {
  try {
    const { wid } = await c.req.json()
    if (!wid) return c.json({ message: 'Missing wid' }, 400)
    const res = await runXdotool(['getwindowgeometry', '--shell', String(wid)])
    const out = res.stdout.toString('utf8').trim().split('\n')
    const kv = {}
    for (const line of out) {
      const [k, v] = line.split('=')
      kv[k] = v
    }
    return c.json({
      wid,
      x: Number(kv['X']),
      y: Number(kv['Y']),
      width: Number(kv['WIDTH']),
      height: Number(kv['HEIGHT']),
      screen: Number(kv['SCREEN']),
    })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

// ---------- desktop / screen ----------
inputRouter.get('/input/display/geometry', async (c) => {
  try {
    const res = await runXdotool(['getdisplaygeometry'])
    const [widthStr, heightStr] = res.stdout.toString('utf8').trim().split(' ')
    return c.json({ width: Number(widthStr), height: Number(heightStr) })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.get('/input/desktop/count', async (c) => {
  try {
    const res = await runXdotool(['get_num_desktops'])
    return c.json({ count: Number(res.stdout.toString('utf8').trim()) })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.post('/input/desktop/count', async (c) => {
  try {
    const { count } = await c.req.json()
    await runXdotool(['set_num_desktops', String(count)])
    return c.json({ ok: true, count })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.get('/input/desktop/current', async (c) => {
  try {
    const res = await runXdotool(['get_desktop'])
    return c.json({ index: Number(res.stdout.toString('utf8').trim()) })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.post('/input/desktop/current', async (c) => {
  try {
    const { index } = await c.req.json()
    await runXdotool(['set_desktop', String(index)])
    return c.json({ ok: true, index })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.post('/input/desktop/window_desktop', async (c) => {
  try {
    const { match, index } = await c.req.json()
    const wid = await findWindowId(match)
    if (!wid) return c.json({ message: 'Not Found' }, 404)
    await runXdotool(['set_desktop_for_window', wid, String(index)])
    return c.json({ ok: true, wid, index })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.post('/input/desktop/viewport', async (c) => {
  try {
    const { x, y } = await c.req.json()
    await runXdotool(['set_desktop_viewport', String(x), String(y)])
    return c.json({ ok: true, x, y })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.get('/input/desktop/viewport', async (c) => {
  try {
    const res = await runXdotool(['get_desktop_viewport'])
    const [xStr, yStr] = res.stdout.toString('utf8').trim().split(' ')
    return c.json({ x: Number(xStr), y: Number(yStr) })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

// ---------- combos / convenience ----------
inputRouter.post('/input/combo/activate_and_type', async (c) => {
  try {
    const { match, text, enter = false, wpm = 300 } = await c.req.json()
    const wid = await findWindowId(match)
    if (!wid) return c.json({ message: 'Not Found' }, 404)
    await runXdotool(['windowactivate', wid])
    const delay = Math.max(1, Math.round(60000 / (wpm * 5)))
    await runXdotool(['type', '--delay', String(delay), '--clearmodifiers', '--', String(text ?? '')])
    if (enter) await runXdotool(['key', 'Return'])
    return c.json({ ok: true, wid })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.post('/input/combo/activate_and_keys', async (c) => {
  try {
    const { match, keys } = await c.req.json()
    const wid = await findWindowId(match)
    if (!wid) return c.json({ message: 'Not Found' }, 404)
    await runXdotool(['windowactivate', wid])
    const seq = Array.isArray(keys) ? keys : [asString(keys)]
    if (!seq.length || !seq[0]) return c.json({ message: 'Missing keys' }, 400)
    await runXdotool(['key', ...seq])
    return c.json({ ok: true, wid })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.post('/input/combo/window/center', async (c) => {
  try {
    const { match, width, height } = await c.req.json()
    const wid = await findWindowId(match)
    if (!wid) return c.json({ message: 'Not Found' }, 404)

    const disp = await runXdotool(['getdisplaygeometry'])
    const [dw, dh] = disp.stdout.toString('utf8').trim().split(' ').map(Number)

    let w = width, h = height
    if (w === undefined || h === undefined) {
      const geo = await runXdotool(['getwindowgeometry', '--shell', wid])
      const lines = geo.stdout.toString('utf8').trim().split('\n')
      const kv = {}
      for (const line of lines) {
        const [k, v] = line.split('=')
        kv[k] = Number(v)
      }
      w = w ?? kv['WIDTH']
      h = h ?? kv['HEIGHT']
    }

    const x = Math.max(0, Math.round((dw - Number(w)) / 2))
    const y = Math.max(0, Math.round((dh - Number(h)) / 2))
    await runXdotool(['windowmove', wid, String(x), String(y)])
    if (width !== undefined && height !== undefined) {
      await runXdotool(['windowsize', wid, String(width), String(height)])
    }
    return c.json({ ok: true, wid, x, y, width: Number(w), height: Number(h) })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.post('/input/combo/window/snap', async (c) => {
  try {
    const { match, position } = await c.req.json()
    const wid = await findWindowId(match)
    if (!wid) return c.json({ message: 'Not Found' }, 404)

    const disp = await runXdotool(['getdisplaygeometry'])
    const [dw, dh] = disp.stdout.toString('utf8').trim().split(' ').map(Number)

    let x = 0, y = 0, w = dw, h = dh
    switch (position) {
      case 'left': w = Math.floor(dw / 2); h = dh; x = 0; y = 0; break
      case 'right': w = Math.floor(dw / 2); h = dh; x = dw - w; y = 0; break
      case 'top': w = dw; h = Math.floor(dh / 2); x = 0; y = 0; break
      case 'bottom': w = dw; h = Math.floor(dh / 2); x = 0; y = dh - h; break
      case 'topleft': w = Math.floor(dw / 2); h = Math.floor(dh / 2); x = 0; y = 0; break
      case 'topright': w = Math.floor(dw / 2); h = Math.floor(dh / 2); x = dw - w; y = 0; break
      case 'bottomleft': w = Math.floor(dw / 2); h = Math.floor(dh / 2); x = 0; y = dh - h; break
      case 'bottomright': w = Math.floor(dw / 2); h = Math.floor(dh / 2); x = dw - w; y = dh - h; break
      case 'maximize': w = dw; h = dh; x = 0; y = 0; break
      default: return c.json({ message: 'Invalid position' }, 400)
    }
    await runXdotool(['windowmove', wid, String(x), String(y)])
    await runXdotool(['windowsize', wid, String(w), String(h)])
    return c.json({ ok: true, wid, x, y, width: w, height: h })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

// ---------- process / exec / sleep ----------
inputRouter.post('/input/system/exec', async (c) => {
  try {
    const { command, args = [] } = await c.req.json()
    if (!command) return c.json({ message: 'Missing command' }, 400)
    const res = await execCapture(String(command), args.map(String))
    return c.json({
      code: res.code,
      stdout: res.stdout.toString('utf8'),
      stderr: res.stderr.toString('utf8'),
    })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

inputRouter.post('/input/system/sleep', async (c) => {
  try {
    const { seconds } = await c.req.json()
    await runXdotool(['sleep', String(Number(seconds) || 0)])
    return c.json({ ok: true })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})

// ---------- aliases for backward compat ----------
inputRouter.post('/computer/move_mouse', async (c) => inputRouter.routes.get('/input/mouse/move')(c))
inputRouter.post('/computer/click_mouse', async (c) => {
  try {
    const body = await c.req.json()
    const button = buttonNumFromName(body.button)
    const clickType = body.click_type || 'click'
    const count = body.num_clicks || 1
    if (clickType === 'down') await runXdotool(['mousedown', String(button)])
    else if (clickType === 'up') await runXdotool(['mouseup', String(button)])
    else await runXdotool(['click', '--repeat', String(count), String(button)])
    return c.json({ ok: true })
  } catch (e) {
    return c.json({ message: String(e.message) }, 400)
  }
})
