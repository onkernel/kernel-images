import 'dotenv/config'
import { execSpawn } from '../utils/exec.js'
import { uid } from '../utils/ids.js'
import { EventEmitter } from 'node:events'

const FFMPEG = process.env.FFMPEG_BIN || '/usr/bin/ffmpeg'
const DISPLAY = process.env.DISPLAY || ':0'
const WIDTH = Number(process.env.WIDTH || 1024)
const HEIGHT = Number(process.env.HEIGHT || 768)
const PULSE_SOURCE = process.env.PULSE_SOURCE || 'default'

const streams = new Map() // id -> {proc, emitter}

export function startStream(req) {
  const stream_id = uid()
  const {
    region = null,
    display = 0,
    fps = 30,
    video_codec = 'h264',
    video_bitrate_kbps = 3500,
    audio = { capture_system: true, capture_mic: false },
    rtmps_url,
    stream_key
  } = req

  if (!rtmps_url || !stream_key) throw new Error('Missing RTMPS params')

  // We can omit video_size as ffmpeg will detect the screen dimensions automatically
  const input = ['-f', 'x11grab', '-r', String(fps), '-i', `${DISPLAY}.0`]
  const audioArgs = audio?.capture_system ? ['-f', 'pulse', '-i', PULSE_SOURCE] : []
  const vf = region ? ['-filter:v', `crop=${region.width}:${region.height}:${region.x}:${region.y}`] : []
  const out = ['-c:v', video_codec === 'h265' ? 'libx265' : video_codec === 'av1' ? 'libsvtav1' : 'libx264', '-b:v', `${video_bitrate_kbps}k`, '-c:a', 'aac', '-f', 'flv', `${rtmps_url}/${stream_key}`]

  const args = ['-thread_queue_size', '512', ...input, ...audioArgs, ...vf, ...out]
  const proc = execSpawn(FFMPEG, ['-hide_banner', ...args], { env: { ...process.env } })
  const emitter = new EventEmitter()
  streams.set(stream_id, { proc, emitter })

  proc.stderr.on('data', (d) => {
    const s = d.toString('utf8')
    // rough parse
    const fpsMatch = s.match(/fps=\s*([\d.]+)/)
    const kbpsMatch = s.match(/bitrate=\s*([\d.]+)kbits\/s/)
    const dropMatch = s.match(/drop=\s*(\d+)/)
    const obj = {
      ts: new Date().toISOString(),
      fps: fpsMatch ? Number(fpsMatch[1]) : undefined,
      bitrate_kbps: kbpsMatch ? Number(kbpsMatch[1]) : undefined,
      dropped_frames: dropMatch ? Number(dropMatch[1]) : undefined
    }
    emitter.emit('metrics', obj)
  })
  proc.on('close', () => {
    emitter.emit('metrics', { ts: new Date().toISOString(), ended: true })
  })

  return { stream_id }
}

export function stopStream({ stream_id }) {
  const item = streams.get(stream_id)
  if (!item) throw new Error('Not Found')
  item.proc.kill('SIGINT')
  return true
}

export function metricsEmitter(stream_id) {
  const item = streams.get(stream_id)
  if (!item) return null
  return item.emitter
}
