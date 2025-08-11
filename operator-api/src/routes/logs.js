import { Hono } from 'hono'
import { spawn } from 'node:child_process'
import { sseHeaders } from '../utils/sse.js'

export const logsRouter = new Hono()

logsRouter.get('/logs/stream', async (c) => {
  const source = c.req.query('source')
  const path = c.req.query('path')
  const follow = c.req.query('follow') !== 'false'
  let proc
  if (source === 'path' && path) {
    proc = spawn('tail', [follow ? '-F' : '-f', path])
  } else if (source === 'kernel') {
    proc = spawn('journalctl', ['-k', '-f'])
  } else if (source === 'syslog') {
    proc = spawn('journalctl', ['-f'])
  } else if (source === 'application') {
    proc = spawn('journalctl', ['-f', '-t', 'application'])
  } else {
    return c.json({ message: 'Bad Request' }, 400)
  }
  const stream = new ReadableStream({
    start(controller) {
      proc.stdout.on('data', (d) => {
        const lines = d.toString('utf8').split('\n').filter(Boolean)
        for (const line of lines) controller.enqueue(new TextEncoder().encode(`event: data\ndata: ${JSON.stringify({ source, line, ts: new Date().toISOString() })}\n\n`))
      })
      proc.on('close', () => controller.close())
    },
    cancel() {
      proc.kill('SIGINT')
    }
  })
  return new Response(stream, { headers: sseHeaders() })
})
