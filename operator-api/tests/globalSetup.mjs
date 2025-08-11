import { spawn } from 'node:child_process'
import { setTimeout as delay } from 'node:timers/promises'
import { request } from 'undici'

const TEST_PORT = Number(process.env.PORT || 10001)
const BASE_URL = `http://127.0.0.1:${TEST_PORT}`

let child

async function waitForHealth(timeoutMs = 20000) {
  const start = Date.now()
  while (Date.now() - start < timeoutMs) {
    try {
      const { statusCode } = await request(`${BASE_URL}/health`, { method: 'GET' })
      if (statusCode === 200) return
    } catch {}
    await delay(300)
  }
  throw new Error('Server did not become healthy')
}

export default async function() {
  child = spawn(process.execPath, ['index.js'], {
    env: { ...process.env, PORT: String(TEST_PORT) },
    stdio: ['ignore', 'pipe', 'pipe']
  })

  // optional log piping to help flakiness diagnosis
  child.stdout.on('data', () => {})
  child.stderr.on('data', () => {})

  await waitForHealth()

  // expose to tests
  globalThis.__TEST_BASE_URL__ = BASE_URL

  return async () => {
    if (child && !child.killed) {
      child.kill('SIGINT')
      // best-effort shutdown
      await delay(500)
      if (!child.killed) child.kill('SIGKILL')
    }
  }
}
