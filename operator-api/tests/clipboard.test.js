import { j, raw } from './utils.js'

describe('clipboard', () => {
  it('GET /clipboard responds even if tooling missing', async () => {
    const r = await j('/clipboard')
    expect(r.status).toBe(200)
    expect(['text', 'image']).toContain(r.body.type)
  })

  it('GET /clipboard/stream returns SSE stream and detects changes', async () => {
    // First set a known value to the clipboard
    const timestamp = Date.now().toString()
    const testText = `Test clipboard content ${timestamp}`
    
    await j('/clipboard', {
      method: 'POST',
      body: JSON.stringify({ type: 'text', text: testText })
    })
    
    // Connect to the stream
    const r = await raw('/clipboard/stream')
    expect(r.status).toBe(200)
    expect(r.headers.get('Content-Type')).toBe('text/event-stream')
    
    // Read from the stream
    const reader = r.body.getReader()
    
    // We should get at least one event with our test content
    let foundTestContent = false
    let attempts = 0
    
    while (!foundTestContent && attempts < 5) {
      const { done, value } = await reader.read()
      if (done) break
      
      const chunk = new TextDecoder().decode(value)
      if (chunk.includes(timestamp)) {
        foundTestContent = true
      }
      
      attempts++;
      // Small delay between reads
      await new Promise(resolve => setTimeout(resolve, 200))
    }
    
    // Clean up
    reader.cancel()
    
    expect(foundTestContent).toBe(true)
  })
})
