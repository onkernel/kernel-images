import fs from 'node:fs'
import path from 'node:path'
import { j, raw, tmpPath } from './utils.js'

describe('fs-nodelete', () => {
  it('PUT /fs/create_directory + list + file_info', async () => {
    const dir = tmpPath('fsdir-nodelete')
    let r = await j('/fs/create_directory', { method: 'PUT', body: JSON.stringify({ path: dir, mode: '0755' }) })
    expect(r.status).toBe(201)
    r = await j(`/fs/list_files?path=${encodeURIComponent(path.dirname(dir))}`)
    expect(r.status).toBe(200)
    expect(Array.isArray(r.body)).toBe(true)
    r = await j(`/fs/file_info?path=${encodeURIComponent(dir)}`)
    expect(r.status).toBe(200)
    expect(r.body.is_dir).toBe(true)
    // No delete operation
  })

  it('PUT /fs/write_file + GET /fs/read_file + download + move', async () => {
    const p = tmpPath('fsfile-nodelete.txt')
    let r = await raw(`/fs/write_file?path=${encodeURIComponent(p)}&mode=0644`, {
      method: 'PUT',
      body: Buffer.from('hello world')
    })
    expect(r.status).toBe(201)

    r = await raw(`/fs/read_file?path=${encodeURIComponent(p)}`)
    expect(r.status).toBe(200)
    expect(await r.text()).toBe('hello world')

    r = await raw(`/fs/download?path=${encodeURIComponent(p)}`)
    expect(r.status).toBe(200)

    const p2 = tmpPath('fsfile2-nodelete.txt')
    r = await j('/fs/move', { method: 'PUT', body: JSON.stringify({ src_path: p, dest_path: p2 }) })
    expect(r.status).toBe(200)
    // No delete operation
  })

  it('POST /fs/upload', async () => {
    const p = tmpPath('upload-nodelete.bin')
    const data = new TextEncoder().encode('payload')
    const form = new FormData()
    form.set('path', p)
    form.set('file', new Blob([data]), 'file.bin')
    const r = await raw('/fs/upload', { method: 'POST', body: form })
    expect(r.status).toBe(200)
    const st = fs.statSync(p)
    expect(st.size).toBe(data.length)
  })
})
