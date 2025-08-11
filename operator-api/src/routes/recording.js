import fs from 'node:fs'
import { Hono } from 'hono'
import { b64 } from '../utils/base64.js'
import {
  startRecording,
  stopRecording,
  getRecorderInfoList,
  getLatestFilePath,
  isRecording,
  deleteRecording
} from '../services/recordingService.js'

export const recordingRouter = new Hono()

recordingRouter.post('/recording/start', async (c) => {
  try {
    const body = await c.req.json().catch(() => ({}))
    const res = startRecording(body || {})
    return c.json({ ok: true, id: res.id, started_at: res.started_at }, 201)
  } catch (e) {
    return c.json({ message: String(e.message || e) }, e.message === 'Already recording' ? 409 : 500)
  }
})

recordingRouter.post('/recording/stop', async (c) => {
  try {
    const body = await c.req.json().catch(() => ({}))
    stopRecording({ id: body?.id || 'default', forceStop: Boolean(body?.forceStop) })
    return c.json({ ok: true })
  } catch (e) {
    return c.json({ message: String(e.message || e) }, 400)
  }
})

recordingRouter.get('/recording/download', async (c) => {
  const id = c.req.query('id') || 'default'
  if (isRecording({ id })) {
    return new Response(null, { status: 202, headers: { 'Retry-After': '3' } })
  }
  const filePath = getLatestFilePath({ id })
  if (!filePath || !fs.existsSync(filePath)) return c.json({ message: 'Not found' }, 404)
  const stat = fs.statSync(filePath)
  const stream = fs.createReadStream(filePath)
  return new Response(stream, {
    status: 200,
    headers: {
      'Content-Type': 'video/mp4',
      'X-Recording-Started-At': new Date(stat.birthtimeMs || stat.ctimeMs).toISOString(),
      'X-Recording-Finished-At': new Date(stat.mtimeMs).toISOString()
    }
  })
})

recordingRouter.get('/recording/list', async (c) => {
  return c.json(getRecorderInfoList())
})

recordingRouter.post('/recording/delete', async (c) => {
  try {
    const body = await c.req.json().catch(() => ({}))
    deleteRecording({ id: body?.id || 'default' })
    return c.json({ ok: true })
  } catch (e) {
    return c.json({ message: String(e.message || e) }, e.message === 'Not found' ? 404 : 400)
  }
})
