import fs from 'node:fs'
import { j, tmpPath } from './utils.js'

const shebang = '#!/usr/bin/env bash\n'

describe('scripts-nodelete', () => {
  it('upload + list + run sync', async () => {
    const scriptPath = tmpPath('script-nodelete.sh')
    const form = new FormData()
    form.set('path', scriptPath)
    form.set('file', new Blob([shebang + 'echo hi']), 'script-nodelete.sh')
    form.set('executable', 'true')
    const up = await j(`/scripts/upload`, { method: 'POST', body: form })
    expect(up.status).toBe(200)

    const list = await j('/scripts/list')
    expect(list.status).toBe(200)
    expect(Array.isArray(list.body.items)).toBe(true)

    const run = await j('/scripts/run', { method: 'POST', body: JSON.stringify({ path: scriptPath }) })
    expect(run.status).toBe(200)
    const out = Buffer.from(run.body.stdout_b64, 'base64').toString('utf8').trim()
    expect(out).toBe('hi')

    // No delete operation
  })
})
