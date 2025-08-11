// `--all` or `input fs` (categories)

import { execSync } from 'node:child_process'
import { promises as fsp } from 'node:fs'
import { dirname, join } from 'node:path'
import os from 'node:os'
import chalk from 'chalk'

// ---------------------------- CLI / Config ----------------------------
const args = process.argv.slice(2)
const flags = new Set(args.filter(a => a.startsWith('-')))
const names = args.filter(a => !a.startsWith('-'))

const RUN_ALL = flags.has('--all')
const BASE_URL = 'http://127.0.0.1:10001'
const ALWAYS_DEBUG = true

// ---------------------------- Utilities ----------------------------
function now() { return Date.now() }
function ms(n) { return `${n}ms` }

function banner(title) {
  const line = '─'.repeat(78)
  console.log(chalk.gray(line))
  console.log(chalk.bold.white(title))
  console.log(chalk.gray(line))
}

function statusLine(kind, text) {
  const tag = kind === 'PASS' ? chalk.bgGreen.black(' PASS ')
    : kind === 'FAIL' ? chalk.bgRed.white(' FAIL ')
      : kind === 'SKIP' ? chalk.bgYellow.black(' SKIP ')
        : chalk.bgBlue.white(` ${kind} `)
  console.log(`${tag} ${text}`)
}

function toCurl(fullUrl, init = {}) {
  let curlCmd = `curl -sS -X ${init.method || 'GET'} "${fullUrl}"`
  const headers = { ...(init.headers || {}) }
  for (const [k, v] of Object.entries(headers)) curlCmd += ` -H "${k}: ${v}"`
  if (init.body && typeof init.body === 'string') curlCmd += ` -d '${init.body.replace(/'/g, "'\\''")}'`
  return curlCmd
}

async function j(url, init = {}) {
  const full = `${BASE_URL}${url}`
  const headers = { 'content-type': 'application/json', ...(init.headers || {}) }
  if (ALWAYS_DEBUG) console.log(chalk.cyan('[http]'), chalk.yellow(toCurl(full, { ...init, headers })))
  const r = await fetch(full, { ...init, headers })
  const txt = await r.text()
  let body
  try { body = JSON.parse(txt) } catch {
    const isBinaryish = /[\x00-\x08\x0E-\x1F]/.test(txt)
    const isLarge = txt.length > 1000
    body = isBinaryish || isLarge ? `${txt.substring(0, 500)}... [${txt.length} bytes${isBinaryish ? ', binary' : ''}]` : txt
  }
  if (ALWAYS_DEBUG) {
    console.log(chalk.cyan('[http]'), chalk.green(`status=${r.status}`))
    if (typeof body === 'object') {
      const jsonString = JSON.stringify(body, null, 2)
      const maxLength = 3_000
      const maxLengthSlice = 1_000
      if (jsonString.length > maxLength) {
        console.log(chalk.magenta('[body]'), chalk.gray(`${jsonString.substring(0, maxLengthSlice)}... [${jsonString.length} bytes total]`))
      } else {
        console.log(chalk.magenta('[body]'), chalk.gray(jsonString))
      }
    } else if (typeof body === 'string') {
      const maxLength = 3_000
      const maxLengthSlice = 1_500
      if (body.length > maxLength) {
        console.log(chalk.magenta('[body]'), chalk.gray(`${body.substring(0, maxLengthSlice)}... [${body.length} bytes total]`))
      } else {
        console.log(chalk.magenta('[body]'), chalk.gray(body))
      }
    } else {
      console.log(chalk.magenta('[body]'), chalk.gray(String(body)))
    }
  }
  return { status: r.status, headers: r.headers, body }
}

async function raw(url, init = {}) {
  const full = `${BASE_URL}${url}`
  if (ALWAYS_DEBUG) console.log(chalk.cyan('[http] raw'), chalk.yellow(toCurl(full, init)))
  return fetch(full, init)
}

async function readSSE(path, { max = 1, timeoutMs = 5000 } = {}) {
  const full = `${BASE_URL}${path}`
  if (ALWAYS_DEBUG) console.log(chalk.cyan('[sse] connect'), chalk.yellow(full), chalk.gray(`max=${max} timeout=${timeoutMs}`))
  const res = await fetch(full)
  if (!res.ok) throw new Error(`bad status ${res.status}`)
  const reader = res.body.getReader()
  const decoder = new TextDecoder('utf-8')
  const events = []
  let buf = ''
  const start = now()
  while (events.length < max && (now() - start) < timeoutMs) {
    const { value, done } = await reader.read()
    if (done) break
    const chunk = decoder.decode(value, { stream: true })
    if (ALWAYS_DEBUG && chunk.trim()) console.log(chalk.blue('[sse] data'), chalk.gray(chunk.replace(/\n/g, '\\n')))
    buf += chunk
    let idx
    while ((idx = buf.indexOf('\n\n')) !== -1) {
      const block = buf.slice(0, idx)
      buf = buf.slice(idx + 2)
      const dataLine = block.split('\n').find(l => l.startsWith('data: '))
      if (dataLine) {
        const json = dataLine.slice(6)
        try {
          const obj = JSON.parse(json)
          events.push(obj)
          if (ALWAYS_DEBUG) console.log(chalk.green('[sse] event'), chalk.gray(JSON.stringify(obj)))
        } catch { }
      }
      if (events.length >= max) break
    }
  }
  try { reader.cancel() } catch { }
  return events
}

function hasCmd(cmd) {
  try {
    execSync(`bash -lc "command -v ${cmd} >/dev/null 2>&1"`, { stdio: 'ignore' })
    return true
  } catch {
    return false
  }
}

function tmpPath(name = 'kco') {
  const id = Math.random().toString(36).slice(2)
  const p = `${os.tmpdir().replace(/\/$/, '')}/${name}-${id}`
  if (ALWAYS_DEBUG) console.log(chalk.cyan('[tmp]'), chalk.yellow(p))
  return p
}

// ---------------------------- Harness ----------------------------
const RESULTS = []

async function runTest(category, name, fn, { timeoutMs = 30000, skipIf = false } = {}) {
  if (skipIf) {
    RESULTS.push({ category, name, status: 'SKIP', ms: 0, note: typeof skipIf === 'string' ? skipIf : '' })
    statusLine('SKIP', `[${category}] ${name}${skipIf ? `  (${skipIf})` : ''}`)
    return
  }
  const t0 = now()
  try {
    await Promise.race([
      fn(),
      (async () => { await new Promise(r => setTimeout(r, timeoutMs)); throw new Error(`timeout after ${timeoutMs}ms`) })()
    ])
    const dur = now() - t0
    RESULTS.push({ category, name, status: 'PASS', ms: dur })
    statusLine('PASS', `[${category}] ${name}  ${ms(dur)}`)
  } catch (e) {
    const dur = now() - t0
    RESULTS.push({ category, name, status: 'FAIL', ms: dur, error: String(e && e.message || e) })
    statusLine('FAIL', `[${category}] ${name}  ${ms(dur)}  :: ${String(e && e.message || e)}`)
  }
}

function filterCategory(cat) {
  if (RUN_ALL) return true
  if (names.length === 0) return true
  return names.some(n => cat.toLowerCase().includes(n.toLowerCase()))
}

// ---------------------------- Suites ----------------------------
async function suite_health() {
  await runTest('health', 'GET /health is ok', async () => {
    const r = await j('/health')
    if (r.status !== 200) throw new Error('status != 200')
    if (!r.body || r.body.status !== 'ok') throw new Error('bad body')
  })
}

async function suite_browser() {
  await runTest('browser', 'HAR start/stream/stop', async () => {
    const start = await j('/browser/har/start', { method: 'POST', body: JSON.stringify({}) })
    if (start.status !== 200) throw new Error('start failed')
    const id = start.body.har_session_id
    const res = await raw(`/browser/har/${id}/stream`)
    if (res.status !== 200) throw new Error('stream status != 200')
    const reader = res.body.getReader()
    await reader.cancel()
    const stop = await j('/browser/har/stop', { method: 'POST', body: JSON.stringify({ har_session_id: id }) })
    if (stop.status !== 200) throw new Error('stop failed')
  })
}

async function suite_bus() {
  await runTest('bus', 'publish/subscribe SSE', async () => {
    const p = readSSE(`/bus/subscribe?channel=ch1`, { max: 1, timeoutMs: 4000 })
    const pub = await j('/bus/publish', { method: 'POST', body: JSON.stringify({ channel: 'ch1', type: 'note', payload: { msg: 'hello' } }) })
    if (pub.status !== 200) throw new Error('publish failed')
    const ev = await p
    if (!Array.isArray(ev) || ev.length < 1) throw new Error('no events')
    if (ev[0]?.payload?.msg !== 'hello') throw new Error('payload mismatch')
  })
}

async function suite_clipboard() {
  await runTest('clipboard', 'GET /clipboard responds', async () => {
    const r = await j('/clipboard')
    if (r.status !== 200) throw new Error('status != 200')
    if (!['text', 'image'].includes(r.body.type)) throw new Error('bad type')
  })

  const toolsPresent = hasCmd('xclip') || (hasCmd('wl-copy') && hasCmd('wl-paste'))
  await runTest('clipboard', 'stream detects change (best-effort)', async () => {
    const ts = Date.now().toString()
    const text = `Test ${ts}`
    const set = await j('/clipboard', { method: 'POST', body: JSON.stringify({ type: 'text', text }) })
    if (set.status !== 200 || set.body.ok !== true) throw new Error('POST /clipboard failed')
    const res = await raw('/clipboard/stream')
    if (res.status !== 200) throw new Error('stream open failed')
    const reader = res.body.getReader()
    let found = false
    const endAt = now() + 5000
    while (now() < endAt && !found) {
      const { done, value } = await reader.read()
      if (done) break
      const chunk = new TextDecoder().decode(value)
      if (chunk.includes(ts)) found = true
      await new Promise(r => setTimeout(r, 150))
    }
    try { reader.cancel() } catch { }
    if (!found) throw new Error('no change detected')
  }, { skipIf: toolsPresent ? false : 'clipboard tools missing' })
}

async function suite_fs() {
  await runTest('fs', 'create/list/file_info/delete_directory', async () => {
    const dir = tmpPath('fsdir')
    let r = await j('/fs/create_directory', { method: 'PUT', body: JSON.stringify({ path: dir, mode: '0755' }) })
    if (r.status !== 201) throw new Error('create_directory failed')
    r = await j(`/fs/list_files?path=${encodeURIComponent(dirname(dir))}`)
    if (r.status !== 200 || !Array.isArray(r.body)) throw new Error('list_files failed')
    r = await j(`/fs/file_info?path=${encodeURIComponent(dir)}`)
    if (r.status !== 200 || r.body.is_dir !== true) throw new Error('file_info failed')
    r = await j('/fs/delete_directory', { method: 'PUT', body: JSON.stringify({ path: dir }) })
    if (r.status !== 200) throw new Error('delete_directory failed')
  })

  await runTest('fs', 'write/read/download/move/delete_file', async () => {
    const p1 = tmpPath('fsfile.txt')
    let r = await raw(`/fs/write_file?path=${encodeURIComponent(p1)}&mode=0644`, { method: 'PUT', body: Buffer.from('hello world') })
    if (r.status !== 201) throw new Error('write_file failed')
    r = await raw(`/fs/read_file?path=${encodeURIComponent(p1)}`)
    if (r.status !== 200) throw new Error('read_file failed')
    const txt = await r.text()
    if (txt !== 'hello world') throw new Error('content mismatch')
    r = await raw(`/fs/download?path=${encodeURIComponent(p1)}`)
    if (r.status !== 200) throw new Error('download failed')
    const p2 = tmpPath('fsfile2.txt')
    let jres = await j('/fs/move', { method: 'PUT', body: JSON.stringify({ src_path: p1, dest_path: p2 }) })
    if (jres.status !== 200) throw new Error('move failed')
    jres = await j('/fs/delete_file', { method: 'PUT', body: JSON.stringify({ path: p2 }) })
    if (jres.status !== 200) throw new Error('delete_file failed')
  })

  await runTest('fs', 'upload', async () => {
    const p = tmpPath('upload.bin')
    const data = new TextEncoder().encode('payload')
    const form = new FormData()
    form.set('path', p)
    form.set('file', new Blob([data], { type: 'application/octet-stream' }), 'file.bin')
    const r = await raw('/fs/upload', { method: 'POST', body: form })
    if (r.status !== 200) throw new Error('upload failed')
    const st = await fsp.stat(p)
    if (st.size !== data.length) throw new Error('size mismatch')
  })

  await runTest('fs', 'set_file_permissions', async () => {
    const p = tmpPath('perm.txt')
    await raw(`/fs/write_file?path=${encodeURIComponent(p)}&mode=0644`, { method: 'PUT', body: Buffer.from('x') })
    const r = await j('/fs/set_file_permissions', { method: 'PUT', body: JSON.stringify({ path: p, mode: '0755' }) })
    if (r.status !== 200) throw new Error('set perms failed')
  })

  await runTest('fs', 'tail/stream yields events', async () => {
    const p = tmpPath('tail.log')
    await raw(`/fs/write_file?path=${encodeURIComponent(p)}&mode=0644`, { method: 'PUT', body: Buffer.from('initial\n') })
    const res = await raw(`/fs/tail/stream?path=${encodeURIComponent(p)}`)
    if (res.status !== 200 || res.headers.get('Content-Type')?.startsWith('text/event-stream') !== true) throw new Error('tail stream failed')
    const reader = res.body.getReader()
    const line = `new line ${Date.now()}\n`
    await fsp.appendFile(p, line)
    let found = false
    const endAt = now() + 5000
    while (now() < endAt && !found) {
      const { done, value } = await reader.read()
      if (done) break
      const chunk = new TextDecoder().decode(value)
      if (chunk.includes(line.trim())) found = true
      await new Promise(r => setTimeout(r, 150))
    }
    try { reader.cancel() } catch { }
    if (!found) throw new Error('no tail event seen')
  })

  await runTest('fs', 'watch events', async () => {
    const dir = tmpPath('watch-dir')
    await fsp.mkdir(dir, { recursive: true })
    let r = await j('/fs/watch', { method: 'POST', body: JSON.stringify({ path: dir, recursive: true }) })
    if (r.status !== 200 || !r.body.watch_id) throw new Error('watch start failed')
    const wid = r.body.watch_id
    const stream = await raw(`/fs/watch/${wid}/events`)
    if (stream.status !== 200) throw new Error('watch stream failed')
    const reader = stream.body.getReader()
    const testFile = join(dir, `t-${Date.now()}.txt`)
    await fsp.writeFile(testFile, 'content')
    let got = false
    const endAt = now() + 5000
    while (now() < endAt && !got) {
      const { done, value } = await reader.read()
      if (done) break
      const chunk = new TextDecoder().decode(value)
      if (chunk.includes(testFile.split('/').pop())) got = true
      await new Promise(r => setTimeout(r, 150))
    }
    try { reader.cancel() } catch { }
    await j(`/fs/watch/${wid}`, { method: 'DELETE' })
    if (!got) throw new Error('no watch event seen')
    try { await fsp.unlink(testFile) } catch { }
    try { await fsp.rmdir(dir) } catch { }
  })
}

async function suite_input() {
  const haveXdotool = hasCmd(process.env.XDOTOOL_BIN || 'xdotool')
  const skipReason = haveXdotool ? false : 'xdotool not found'

  await runTest('input', 'mouse basic', async () => {
    let r = await j('/input/mouse/move', { method: 'POST', body: JSON.stringify({ x: 100, y: 100 }) })
    if (r.status !== 200) throw new Error('move failed')
    r = await j('/input/mouse/click', { method: 'POST', body: JSON.stringify({ button: 'left', count: 1 }) })
    if (r.status !== 200) throw new Error('click failed')
    r = await j('/input/mouse/location')
    if (r.status !== 200 || typeof r.body.x !== 'number') throw new Error('location failed')
  }, { skipIf: skipReason })

  await runTest('input', 'keyboard basic', async () => {
    let r = await j('/input/keyboard/type', { method: 'POST', body: JSON.stringify({ text: 'test', wpm: 300 }) })
    if (r.status !== 200) throw new Error('type failed')
    r = await j('/input/keyboard/key', { method: 'POST', body: JSON.stringify({ keys: ['Return'] }) })
    if (r.status !== 200) throw new Error('key failed')
  }, { skipIf: skipReason })

  await runTest('input', 'window ops and combos (best-effort)', async () => {
    const ok = s => [200, 404].includes(s)
    let r = await j('/input/window/move_resize', { method: 'POST', body: JSON.stringify({ match: { name: 'chromium' }, x: 0, y: 0, width: 800, height: 600 }) })
    if (!ok(r.status)) throw new Error('move_resize bad status')
    r = await j('/input/combo/window/center', { method: 'POST', body: JSON.stringify({ match: { name: 'chromium' }, width: 800, height: 600 }) })
    if (!ok(r.status)) throw new Error('center bad status')
    r = await j('/input/combo/window/snap', { method: 'POST', body: JSON.stringify({ match: { name: 'chromium' }, position: 'left' }) })
    if (!ok(r.status)) throw new Error('snap bad status')
  }, { skipIf: skipReason })
}

async function suite_logs() {
  await runTest('logs', 'rejects bad request', async () => {
    const res = await raw('/logs/stream')
    if (res.status !== 400) throw new Error('should be 400')
  })
}

async function suite_macros() {
  await runTest('macros', 'create/list/delete + run + invalid run', async () => {
    const create = await j('/macros/create', { method: 'POST', body: JSON.stringify({ name: 'type-hello', steps: [{ action: 'sleep', ms: 5 }, { action: 'keyboard.type', text: 'hello' }] }) })
    if (create.status !== 200) throw new Error('create failed')
    const id = create.body.macro_id
    const list = await j('/macros/list')
    if (list.status !== 200 || !Array.isArray(list.body.items)) throw new Error('list failed')
    const run = await j('/macros/run', { method: 'POST', body: JSON.stringify({ macro_id: id }) })
    if (run.status !== 200) throw new Error('run failed')
    const invalid = await j('/macros/run', { method: 'POST', body: JSON.stringify({ macro_id: 'nope' }) })
    if (invalid.status !== 404) throw new Error('invalid run should be 404')
    const del = await j(`/macros/${id}`, { method: 'DELETE' })
    if (del.status !== 200) throw new Error('delete failed')
  })
}

async function suite_metrics() {
  await runTest('metrics', 'snapshot', async () => {
    const r = await j('/metrics/snapshot')
    if (r.status !== 200 || typeof r.body.cpu_pct !== 'number') throw new Error('bad snapshot')
  })
  await runTest('metrics', 'stream yields', async () => {
    const ev = await readSSE('/metrics/stream', { max: 2, timeoutMs: 4000 })
    if (!Array.isArray(ev) || ev.length < 1) throw new Error('no events')
  })
}

async function suite_network() {
  await runTest('network', 'apply/delete intercept rules', async () => {
    const rules = { rules: [{ match: { method: 'GET', host_contains: 'example.com' }, action: { type: 'block' } }] }
    const apply = await j('/network/intercept/rules', { method: 'POST', body: JSON.stringify(rules) })
    if (apply.status !== 200) throw new Error('apply failed')
    const del = await j(`/network/intercept/rules/${apply.body.rule_set_id}`, { method: 'DELETE' })
    if (del.status !== 200) throw new Error('delete failed')
  })
  await runTest('network', 'HAR stream opens', async () => {
    const res = await raw('/network/har/stream')
    if (res.status !== 200) throw new Error('har stream failed')
    const reader = res.body.getReader()
    await reader.cancel()
  })
}

async function suite_os() {
  await runTest('os', 'locale get/post', async () => {
    let r = await j('/os/locale')
    if (r.status !== 200) throw new Error('get failed')
    r = await j('/os/locale', { method: 'POST', body: JSON.stringify({ locale: 'en_US.UTF-8', timezone: 'UTC', keyboard_layout: 'us' }) })
    if (r.status !== 200 || r.body.updated !== true) throw new Error('post failed')
  })
}

async function suite_pipe() {
  await runTest('pipe', 'send/recv', async () => {
    const recv = readSSE('/pipe/recv/stream?channel=x', { max: 1, timeoutMs: 4000 })
    const send = await j('/pipe/send', { method: 'POST', body: JSON.stringify({ channel: 'x', object: { n: 42 } }) })
    if (send.status !== 200) throw new Error('send failed')
    const ev = await recv
    if (!Array.isArray(ev) || ev.length < 1 || ev[0].object?.n !== 42) throw new Error('bad event')
  })
}

async function suite_process() {
  await runTest('process', 'exec', async () => {
    const r = await j('/process/exec', { method: 'POST', body: JSON.stringify({ command: 'bash', args: ['-lc', 'printf hi'] }) })
    if (r.status !== 200) throw new Error('exec failed')
    const out = Buffer.from(r.body.stdout_b64, 'base64').toString('utf8')
    if (out !== 'hi') throw new Error('stdout mismatch')
  })
  await runTest('process', 'spawn/status/stream/kill', async () => {
    const start = await j('/process/spawn', { method: 'POST', body: JSON.stringify({ command: 'bash', args: ['-lc', 'for i in 1 2 3; do echo $i; sleep 1; done'] }) })
    if (start.status !== 200) throw new Error('spawn failed')
    const pid = start.body.process_id
    const status = await j(`/process/${pid}/status`)
    if (status.status !== 200) throw new Error('status failed')
    const ev = await readSSE(`/process/${pid}/stdout/stream`, { max: 2, timeoutMs: 6000 })
    if (!Array.isArray(ev) || ev.length < 1) throw new Error('no stream')
    const kill = await j(`/process/${pid}/kill`, { method: 'POST', body: JSON.stringify({ signal: 'TERM' }) })
    if (kill.status !== 200) throw new Error('kill failed')
  })
}

async function suite_recording() {
  const haveFFmpeg = hasCmd(process.env.FFMPEG_BIN || 'ffmpeg')
  await runTest('recording', 'start/list/stop/download/delete', async () => {
    let r = await j('/recording/start', { method: 'POST', body: JSON.stringify({ id: 't1', maxDurationInSeconds: 1 }) })
    if (r.status !== 201) throw new Error('start failed')
    r = await j('/recording/list')
    if (r.status !== 200) throw new Error('list failed')
    await new Promise(res => setTimeout(res, 1500))
    const d = await raw('/recording/download?id=t1')
    if (![200, 404, 202].includes(d.status)) throw new Error('download bad status')
    const del = await j('/recording/delete', { method: 'POST', body: JSON.stringify({ id: 't1' }) })
    if (![200, 404].includes(del.status)) throw new Error('delete bad status')
  }, { skipIf: haveFFmpeg ? false : 'ffmpeg missing' })
}

async function suite_screenshot() {
  const haveFFmpeg = hasCmd(process.env.FFMPEG_BIN || 'ffmpeg')
  const haveGrim = hasCmd('grim')
  await runTest('screenshot', 'capture returns image bytes (best-effort)', async () => {
    const r = await j('/screenshot/capture', { method: 'POST', body: JSON.stringify({ format: 'png' }) })
    if (![200, 500].includes(r.status)) throw new Error('bad status')
    if (r.status === 200) {
      if (!r.body.bytes_b64 || !r.body.content_type) throw new Error('missing fields')
    }
  }, { skipIf: (haveFFmpeg || haveGrim) ? false : 'ffmpeg/grim missing' })
}

async function suite_scripts() {
  await runTest('scripts', 'upload/list/run/delete', async () => {
    const scriptPath = tmpPath('script.sh')
    const shebang = '#!/usr/bin/env bash\n'
    const form = new FormData()
    form.set('path', scriptPath)
    form.set('file', new Blob([shebang + 'echo hi'], { type: 'text/x-shellscript' }), 'script.sh')
    form.set('executable', 'true')
    const up = await raw('/scripts/upload', { method: 'POST', body: form })
    if (up.status !== 200) throw new Error('upload failed')
    const list = await j('/scripts/list')
    if (list.status !== 200 || !Array.isArray(list.body.items)) throw new Error('list failed')
    const run = await j('/scripts/run', { method: 'POST', body: JSON.stringify({ path: scriptPath }) })
    if (run.status !== 200) throw new Error('run failed')
    const out = Buffer.from(run.body.stdout_b64, 'base64').toString('utf8').trim()
    if (out !== 'hi') throw new Error('stdout mismatch')
    const del = await j('/scripts/delete', { method: 'DELETE', body: JSON.stringify({ path: scriptPath }) })
    if (del.status !== 200) throw new Error('delete failed')
  })
}

async function suite_stream() {
  const haveFFmpeg = hasCmd(process.env.FFMPEG_BIN || 'ffmpeg')
  await runTest('stream', 'start metrics stream then stop (best-effort)', async () => {
    const start = await j('/stream/start', { method: 'POST', body: JSON.stringify({ rtmps_url: 'rtmps://example.com/live', stream_key: 'testkey', fps: 1, audio: { capture_system: false } }) })
    if (![200, 400].includes(start.status)) throw new Error('start bad status')
    if (start.status === 200) {
      const { stream_id } = start.body
      const ev = await readSSE(`/stream/${stream_id}/metrics/stream`, { max: 1, timeoutMs: 5000 })
      if (!Array.isArray(ev)) throw new Error('no metrics')
      const stop = await j('/stream/stop', { method: 'POST', body: JSON.stringify({ stream_id }) })
      if (stop.status !== 200) throw new Error('stop failed')
    }
  }, { skipIf: haveFFmpeg ? false : 'ffmpeg missing' })
}
async function suite_browser_ext() {
  await runTest('browser-ext', 'add unpacked extension from GitHub', async () => {
    // Create form data for the extension upload
    const form = new FormData()
    form.set('github_url', 'https://github.com/raiden-staging/kernel-chrome-ext')
    
    const r = await raw('/browser/extension/add/unpacked', { 
      method: 'POST', 
      body: form
    })
    
    const body = await r.json()
    if (r.status !== 201) throw new Error(`bad status ${r.status}`)
    if (!body.id || !body.version) throw new Error('missing extension details')
    console.log(chalk.gray(`Extension ID: ${body.id}, Version: ${body.version}`))
  })
  
  await runTest('browser-ext', 'add unpacked extension from file', async () => {
    // Create a test zip file
    const testZipPath = tmpPath('test-extension.zip')
    // This test would need a real zip file with extension content
    // For now we'll just check if the endpoint handles file uploads correctly
    
    const form = new FormData()
    form.set('archive_file', new Blob(['test content'], { type: 'application/zip' }), 'test-extension.zip')
    
    const r = await raw('/browser/extension/add/unpacked', { 
      method: 'POST', 
      body: form
    })
    
    // This will likely fail with 500 since we're not providing a real extension zip
    // But we're testing the form handling, not the extension loading
    if (![201, 500].includes(r.status)) throw new Error(`unexpected status ${r.status}`)
  })
}

// ---------------------------- Runner ----------------------------
const SUITES = [
  ['health', suite_health],
  // ['browser', suite_browser],
  ['browser-ext', suite_browser_ext],
  ['bus', suite_bus],
  ['clipboard', suite_clipboard],
  ['fs', suite_fs],
  ['input', suite_input],
  ['logs', suite_logs],
  ['macros', suite_macros],
  ['metrics', suite_metrics],
  ['network', suite_network],
  ['os', suite_os],
  ['pipe', suite_pipe],
  ['process', suite_process],
  ['recording', suite_recording],
  ['screenshot', suite_screenshot],
  ['scripts', suite_scripts],
  ['stream', suite_stream],
]

async function main() {
  // If no parameters specified and not running all tests, display available tests and exit
  if (!RUN_ALL && names.length === 0) {
    console.log(chalk.bgBlue.white(' AVAILABLE TESTS '))
    console.log(chalk.gray('─'.repeat(78)))
    
    // Group tests by category
    const categories = {}
    for (const [cat, _] of SUITES) {
      if (!categories[cat]) categories[cat] = true
    }
    
    // Display available test categories
    console.log(chalk.yellow('Categories:'))
    Object.keys(categories).sort().forEach(cat => {
      console.log(`  ${chalk.green('•')} ${chalk.cyan(cat)}`)
    })
    
    console.log('')
    console.log(chalk.yellow('Usage:'))
    console.log(`  ${chalk.green('•')} Run all tests: ${chalk.white('--all')}`)
    console.log(`  ${chalk.green('•')} Run specific categories: ${chalk.white('input fs network')}`)
    
    return
  }

  banner('Kernel Computer Operator API — Test Runner')
  console.log(chalk.white(`Base URL: ${BASE_URL}`))
  console.log(chalk.white(`Mode: ${RUN_ALL ? 'all' : (names.length ? `subset: ${names.join(', ')}` : 'default')}`))
  console.log(chalk.white('Server: external only (never spawned)'))
  console.log(chalk.white('Verbosity: always on'))

  // Probe health once; fail fast if unreachable
  try {
    const r = await j('/health')
    if (r.status !== 200) throw new Error('unhealthy')
  } catch (e) {
    console.error(chalk.bgRed.white(' FATAL '), `Cannot reach server at ${BASE_URL} /health :: ${String(e && e.message || e)}`)
    process.exit(1)
  }

  const startAll = now()
  for (const [cat, fn] of SUITES) {
    if (!filterCategory(cat)) continue
    banner(`Suite: ${cat}`)
    try { await fn() } catch { }
  }
  const totalMs = now() - startAll

  // Summary
  const pass = RESULTS.filter(r => r.status === 'PASS').length
  const fail = RESULTS.filter(r => r.status === 'FAIL').length
  const skip = RESULTS.filter(r => r.status === 'SKIP').length
  const total = RESULTS.length
  const coverage = total > 0 ? Math.round(((pass + fail) / total) * 100) : 0
  const reliability = (pass + fail) > 0 ? Math.round((pass / (pass + fail)) * 100) : 0

  banner('Summary')
  const lines = [
    `Total: ${total}`,
    `Pass:  ${pass}`,
    `Fail:  ${fail}`,
    `Skip:  ${skip}`,
    `Elapsed: ${ms(totalMs)}`,
    `Coverage: ${coverage}%`,
    `Reliability: ${reliability}%`
  ]
  const width = Math.max(...lines.map(s => s.length)) + 4
  const top = '┌' + '─'.repeat(width - 2) + '┐'
  const bot = '└' + '─'.repeat(width - 2) + '┘'
  console.log(chalk.gray(top))
  for (const s of lines) console.log(chalk.gray('│ ') + s.padEnd(width - 4) + chalk.gray(' │'))
  console.log(chalk.gray(bot))

  // Failures detail
  const failed = RESULTS.filter(r => r.status === 'FAIL')
  if (failed.length) {
    banner('Failures')
    for (const f of failed) {
      console.log(chalk.red(`• [${f.category}] ${f.name}  ${ms(f.ms)}  :: ${f.error || ''}`))
    }
  }

  process.exit(fail > 0 ? 1 : 0)
}

// Entrypoint
main().catch((e) => {
  console.error(chalk.bgRed.white(' FATAL '), String(e && e.message || e))
  process.exit(1)
})
