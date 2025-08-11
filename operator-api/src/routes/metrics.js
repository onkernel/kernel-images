import { Hono } from 'hono'
import os from 'node:os'
import pidusage from 'pidusage'
import { sseHeaders, sseFormat } from '../utils/sse.js'

export const metricsRouter = new Hono()

function snapshot() {
  const memUsed = os.totalmem() - os.freemem()
  return {
    cpu_pct: 0,
    gpu_pct: 0,
    mem: { used_bytes: memUsed, total_bytes: os.totalmem() },
    disk: { read_bps: 0, write_bps: 0 },
    net: { rx_bps: 0, tx_bps: 0 }
  }
}

metricsRouter.get('/metrics/snapshot', async (c) => {
  return c.json(snapshot())
})

metricsRouter.get('/metrics/stream', async (c) => {
  const stream = new ReadableStream({
    start(controller) {
      const enc = new TextEncoder()
      const iv = setInterval(() => {
        controller.enqueue(enc.encode(sseFormat({ ts: new Date().toISOString(), ...snapshot() })))
      }, 1000)
    },
    cancel() {}
  })
  return new Response(stream, { headers: sseHeaders() })
})
