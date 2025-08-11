import { j } from './utils.js'

describe('network', () => {
  it('start/stop socks5 proxy', async () => {
    const start = await j('/network/proxy/socks5/start', { method: 'POST', body: JSON.stringify({ bind_host: '127.0.0.1', bind_port: 0 }) })
    expect(start.status).toBe(200)
    expect(start.body).toHaveProperty('proxy_id')
    const stop = await j('/network/proxy/socks5/stop', { method: 'POST', body: JSON.stringify({ proxy_id: start.body.proxy_id }) })
    expect(stop.status).toBe(200)
  })

  it('apply/delete intercept rules', async () => {
    const rules = {
      rules: [
        { match: { method: 'GET', host_contains: 'example.com' }, action: { type: 'block' } }
      ]
    }
    const apply = await j('/network/intercept/rules', { method: 'POST', body: JSON.stringify(rules) })
    expect(apply.status).toBe(200)
    const del = await j(`/network/intercept/rules/${apply.body.rule_set_id}`, { method: 'DELETE' })
    expect(del.status).toBe(200)
  })

  it('HAR stream opens', async () => {
    // const res = await fetch(`${globalThis.__TEST_BASE_URL__}/network/har/stream`)
    const res = await fetch(`/network/har/stream`)
    expect(res.status).toBe(200)
    // close immediately
    const reader = res.body.getReader()
    await reader.cancel()
  })
})
