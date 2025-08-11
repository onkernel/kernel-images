import fs from 'node:fs'
import path from 'node:path'
import { j, raw, tmpPath } from './utils.js'

describe('fs', () => {
  it('PUT /fs/create_directory + list + file_info + delete_directory', async () => {
    const dir = tmpPath('fsdir')
    let r = await j('/fs/create_directory', { method: 'PUT', body: JSON.stringify({ path: dir, mode: '0755' }) })
    expect(r.status).toBe(201)
    r = await j(`/fs/list_files?path=${encodeURIComponent(path.dirname(dir))}`)
    expect(r.status).toBe(200)
    expect(Array.isArray(r.body)).toBe(true)
    r = await j(`/fs/file_info?path=${encodeURIComponent(dir)}`)
    expect(r.status).toBe(200)
    expect(r.body.is_dir).toBe(true)
    r = await j('/fs/delete_directory', { method: 'PUT', body: JSON.stringify({ path: dir }) })
    expect(r.status).toBe(200)
  })

  it('PUT /fs/write_file + GET /fs/read_file + download + move + delete_file', async () => {
    const p = tmpPath('fsfile.txt')
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

    const p2 = tmpPath('fsfile2.txt')
    r = await j('/fs/move', { method: 'PUT', body: JSON.stringify({ src_path: p, dest_path: p2 }) })
    expect(r.status).toBe(200)

    r = await j('/fs/delete_file', { method: 'PUT', body: JSON.stringify({ path: p2 }) })
    expect(r.status).toBe(200)
  })

  it('POST /fs/upload', async () => {
    const p = tmpPath('upload.bin')
    const data = new TextEncoder().encode('payload')
    const form = new FormData()
    form.set('path', p)
    form.set('file', new Blob([data]), 'file.bin')
    const r = await raw('/fs/upload', { method: 'POST', body: form })
    expect(r.status).toBe(200)
    const st = fs.statSync(p)
    expect(st.size).toBe(data.length)
  })

  it('PUT /fs/set_file_permissions', async () => {
    const p = tmpPath('permissions.txt')
    await raw(`/fs/write_file?path=${encodeURIComponent(p)}&mode=0644`, {
      method: 'PUT',
      body: Buffer.from('test content')
    })
    
    const r = await j('/fs/set_file_permissions', { 
      method: 'PUT', 
      body: JSON.stringify({ path: p, mode: '0755' }) 
    })
    expect(r.status).toBe(200)
    
    const stats = fs.statSync(p)
    expect((stats.mode & 0o777).toString(8)).toBe('755')
    
    // Clean up
    fs.unlinkSync(p)
  })

  it('GET /fs/tail/stream', async () => {
    const p = tmpPath('tail-test.log')
    await raw(`/fs/write_file?path=${encodeURIComponent(p)}&mode=0644`, {
      method: 'PUT',
      body: Buffer.from('initial line\n')
    })
    
    // Start the tail stream
    const r = await raw(`/fs/tail/stream?path=${encodeURIComponent(p)}`)
    expect(r.status).toBe(200)
    expect(r.headers.get('Content-Type')).toBe('text/event-stream')
    
    const reader = r.body.getReader()
    
    // Append to the file
    const testLine = `new line ${Date.now()}\n`
    fs.appendFileSync(p, testLine)
    
    // Read from the stream
    let foundNewLine = false
    let attempts = 0
    
    while (!foundNewLine && attempts < 5) {
      const { done, value } = await reader.read()
      if (done) break
      
      const chunk = new TextDecoder().decode(value)
      if (chunk.includes(testLine.trim())) {
        foundNewLine = true
      }
      
      attempts++;
      await new Promise(resolve => setTimeout(resolve, 200))
    }
    
    // Clean up
    reader.cancel()
    fs.unlinkSync(p)
    
    expect(foundNewLine).toBe(true)
  })

  it('GET /fs/watch and /fs/watch/{watch_id}/events', async () => {
    const dir = tmpPath('watch-dir')
    fs.mkdirSync(dir, { recursive: true })
    
    // Start watching the directory
    let r = await j('/fs/watch', { 
      method: 'POST', 
      body: JSON.stringify({ path: dir, recursive: true }) 
    })
    expect(r.status).toBe(200)
    expect(r.body).toHaveProperty('watch_id')
    
    const watchId = r.body.watch_id
    
    // Connect to the events stream
    const eventsStream = await raw(`/fs/watch/${watchId}/events`)
    expect(eventsStream.status).toBe(200)
    expect(eventsStream.headers.get('Content-Type')).toBe('text/event-stream')
    
    const reader = eventsStream.body.getReader()
    
    // Create a file in the watched directory
    const testFile = path.join(dir, `test-${Date.now()}.txt`)
    fs.writeFileSync(testFile, 'test content')
    
    // Check if we receive the event
    let foundEvent = false
    let attempts = 0
    
    while (!foundEvent && attempts < 5) {
      const { done, value } = await reader.read()
      if (done) break
      
      const chunk = new TextDecoder().decode(value)
      if (chunk.includes(path.basename(testFile))) {
        foundEvent = true
      }
      
      attempts++;
      await new Promise(resolve => setTimeout(resolve, 200))
    }
    
    // Clean up
    reader.cancel()
    
    // Stop watching
    r = await j(`/fs/watch/${watchId}`, { method: 'DELETE' })
    expect(r.status).toBe(200)
    
    // Clean up files
    fs.unlinkSync(testFile)
    fs.rmdirSync(dir)
    
    expect(foundEvent).toBe(true)
  })
})
