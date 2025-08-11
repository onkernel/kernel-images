import 'dotenv/config'
import fs from 'node:fs'
import path from 'node:path'
import { spawnSync } from 'node:child_process'
import { execSpawn } from '../utils/exec.js'
import { RECORDINGS_DIR } from '../utils/env.js'
import { uid } from '../utils/ids.js'

const FFMPEG = process.env.FFMPEG_BIN || '/usr/bin/ffmpeg'
const WIDTH = Number(process.env.WIDTH || 1024)
const HEIGHT = Number(process.env.HEIGHT || 768)
const DISPLAY = process.env.DISPLAY || ':0'

const state = new Map() // id -> {proc, file, started_at, finished_at}

// Best-effort discovery of a PulseAudio monitor for "what-you-hear" capture.
let _cachedPulseMonitor = undefined
function detectPulseMonitor() {
  if (_cachedPulseMonitor !== undefined) return _cachedPulseMonitor
  const env = { ...process.env }
  try {
    // 1) Try the real default sink -> "<sink>.monitor"
    let sink = null
    const getSink = spawnSync('pactl', ['get-default-sink'], { env, encoding: 'utf8' })
    if (getSink.status === 0) sink = getSink.stdout.trim()
    if (!sink) {
      // Older PulseAudio: parse "pactl info"
      const info = spawnSync('pactl', ['info'], { env, encoding: 'utf8' })
      if (info.status === 0) {
        const m = info.stdout.match(/Default Sink:\s*(.+)\s*$/m)
        if (m) sink = m[1].trim()
      }
    }
    const list = spawnSync('pactl', ['list', 'short', 'sources'], { env, encoding: 'utf8' })
    const sourcesTxt = list.status === 0 ? list.stdout : ''
    const hasSource = (name) => sourcesTxt.split('\n').some((ln) => ln.split('\t')[1] === name)
    if (sink && hasSource(`${sink}.monitor`)) {
      _cachedPulseMonitor = `${sink}.monitor`
      return _cachedPulseMonitor
    }
    // 2) Project's common virtual sink name
    if (hasSource('audio_output.monitor')) {
      _cachedPulseMonitor = 'audio_output.monitor'
      return _cachedPulseMonitor
    }
  } catch (_) {
    // ignore
  }
  _cachedPulseMonitor = null
  return null
}

function buildArgsMp4({ framerate = 20, maxDurationInSeconds, audio = true }) {
  const args = ['-nostdin', '-hide_banner']
  // Video input (x11grab)
  args.push('-f', 'x11grab')
  // Width/height omitted intentionally; X server provides exact geometry
  args.push('-i', `${DISPLAY}.0`)
  // Optional audio input (PulseAudio "monitor" of output)
  const pulseMonitor = audio ? detectPulseMonitor() : null
  if (pulseMonitor) {
    args.push('-f', 'pulse', '-i', pulseMonitor)
  }
  // Output encoders and common options
  args.push(
    '-r', String(framerate),
    '-c:v', 'libx264',
    '-preset', 'veryfast',
    '-pix_fmt', 'yuv420p',
    '-movflags', '+faststart'
  )
  if (maxDurationInSeconds) args.push('-t', String(maxDurationInSeconds))
  if (pulseMonitor) {
    // AAC in MP4; 48k stereo; stop when the shorter stream ends
    args.push('-c:a', 'aac', '-b:a', '128k', '-ac', '2', '-ar', '48000', '-shortest')
  }
  return args
}

export function startRecording({ id, framerate, maxDurationInSeconds, maxFileSizeInMB }) {
  const recId = id || 'default'
  if (state.get(recId)?.proc) throw new Error('Already recording')
  const fileName = `${recId}-${Date.now()}.mp4`
  const outPath = path.join(RECORDINGS_DIR, fileName)
  const args = buildArgsMp4({ framerate, maxDurationInSeconds })
  if (maxFileSizeInMB) args.push('-fs', String(maxFileSizeInMB * 1024 * 1024))
  args.push(outPath)
  const proc = execSpawn(FFMPEG, ['-y', ...args], { env: { ...process.env } })
  const started_at = new Date().toISOString()
  state.set(recId, { proc, file: outPath, started_at, finished_at: null })
  proc.on('close', () => {
    const rec = state.get(recId)
    if (rec) {
      rec.finished_at = new Date().toISOString()
      rec.proc = null
      state.set(recId, rec)
    }
  })
  return { id: recId, file: outPath, started_at }
}

export function stopRecording({ id = 'default', forceStop = false }) {
  const rec = state.get(id)
  if (!rec?.proc) throw new Error('Not recording')
  if (forceStop) rec.proc.kill('SIGKILL')
  else rec.proc.kill('SIGINT')
  return true
}

export function getRecorderInfoList() {
  const items = []
  for (const [id, r] of state.entries()) {
    items.push({
      id,
      isRecording: Boolean(r.proc),
      started_at: r.started_at || null,
      finished_at: r.finished_at || null
    })
  }
  if (!items.length) {
    items.push({ id: 'default', isRecording: false, started_at: null, finished_at: null })
  }
  return items
}

export function getLatestFilePath({ id }) {
  const rec = state.get(id || 'default')
  if (!rec) return null
  return rec.file
}

export function isRecording({ id }) {
  const rec = state.get(id || 'default')
  return Boolean(rec?.proc)
}

export function deleteRecording({ id }) {
  const rec = state.get(id || 'default')
  if (!rec?.file) throw new Error('Not found')
  fs.rmSync(rec.file, { force: true })
  return true
}
