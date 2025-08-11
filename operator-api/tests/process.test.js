import { j, readSSE } from './utils.js'

describe('process', () => {
  it('POST /process/exec executes command', async () => {
    const { status, body } = await j('/process/exec', {
      method: 'POST',
      body: JSON.stringify({ command: 'bash', args: ['-lc', 'printf hi'] })
    })
    expect(status).toBe(200)
    const out = Buffer.from(body.stdout_b64, 'base64').toString('utf8')
    expect(out).toBe('hi')
  })

  it('spawn -> status -> stream -> kill', async () => {
    const start = await j('/process/spawn', {
      method: 'POST',
      body: JSON.stringify({ command: 'bash', args: ['-lc', 'for i in 1 2 3; do echo $i; sleep 1; done'] })
    })
    expect(start.status).toBe(200)
    const { process_id } = start.body

    const status1 = await j(`/process/${process_id}/status`)
    expect(status1.status).toBe(200)
    expect(['running', 'exited']).toContain(status1.body.state)

    const events = await readSSE(`/process/${process_id}/stdout/stream`, { max: 2, timeoutMs: 6000 })
    expect(events.length).toBeGreaterThanOrEqual(1)

    const kill = await j(`/process/${process_id}/kill`, { method: 'POST', body: JSON.stringify({ signal: 'TERM' }) })
    expect(kill.status).toBe(200)
  })
})
