import { j } from './utils.js'

describe('input', () => {
  // Mouse endpoints
  describe('mouse', () => {
    it('input/mouse/move', async () => {
      const r = await j('/input/mouse/move', { method: 'POST', body: JSON.stringify({ x: 100, y: 100 }) })
      expect(r.status).toBe(200)
      expect(r.body.ok).toBe(true)
    })

    it('input/mouse/move_relative', async () => {
      const r = await j('/input/mouse/move_relative', { method: 'POST', body: JSON.stringify({ dx: 10, dy: 10 }) })
      expect(r.status).toBe(200)
      expect(r.body.ok).toBe(true)
    })

    it('input/mouse/click', async () => {
      const r = await j('/input/mouse/click', { method: 'POST', body: JSON.stringify({ button: 'left', count: 1 }) })
      expect(r.status).toBe(200)
      expect(r.body.ok).toBe(true)
    })

    it('input/mouse/down', async () => {
      const r = await j('/input/mouse/down', { method: 'POST', body: JSON.stringify({ button: 'left' }) })
      expect(r.status).toBe(200)
      expect(r.body.ok).toBe(true)
    })

    it('input/mouse/up', async () => {
      const r = await j('/input/mouse/up', { method: 'POST', body: JSON.stringify({ button: 'left' }) })
      expect(r.status).toBe(200)
      expect(r.body.ok).toBe(true)
    })

    it('input/mouse/scroll', async () => {
      const r = await j('/input/mouse/scroll', { method: 'POST', body: JSON.stringify({ dx: 0, dy: -120 }) })
      expect(r.status).toBe(200)
      expect(r.body.ok).toBe(true)
    })

    it('input/mouse/location', async () => {
      const r = await j('/input/mouse/location', { method: 'GET' })
      expect(r.status).toBe(200)
      expect(r.body.x).toBeDefined()
      expect(r.body.y).toBeDefined()
    })
  })

  // Keyboard endpoints
  describe('keyboard', () => {
    it('input/keyboard/type', async () => {
      const r = await j('/input/keyboard/type', { method: 'POST', body: JSON.stringify({ text: 'test', wpm: 300 }) })
      expect(r.status).toBe(200)
      expect(r.body.ok).toBe(true)
    })

    it('input/keyboard/key', async () => {
      const r = await j('/input/keyboard/key', { method: 'POST', body: JSON.stringify({ keys: ['Return'] }) })
      expect(r.status).toBe(200)
      expect(r.body.ok).toBe(true)
    })

    it('input/keyboard/key_down', async () => {
      const r = await j('/input/keyboard/key_down', { method: 'POST', body: JSON.stringify({ key: 'ctrl' }) })
      expect(r.status).toBe(200)
      expect(r.body.ok).toBe(true)
    })

    it('input/keyboard/key_up', async () => {
      const r = await j('/input/keyboard/key_up', { method: 'POST', body: JSON.stringify({ key: 'ctrl' }) })
      expect(r.status).toBe(200)
      expect(r.body.ok).toBe(true)
    })
  })

  // Window endpoints
  describe('window', () => {
    it('input/window/activate', async () => {
      const r = await j('/input/window/activate', { method: 'POST', body: JSON.stringify({ match: { name: "chromium" } }) })
      expect(r.status).toBe(200)
    })

    it('input/window/focus', async () => {
      const r = await j('/input/window/focus', { method: 'POST', body: JSON.stringify({ match: { name: "chromium" } }) })
      expect(r.status).toBe(200)
    })

    it('input/window/move_resize', async () => {
      const r = await j('/input/window/move_resize', { 
        method: 'POST', 
        body: JSON.stringify({ match: { name: "chromium" }, x: 0, y: 0, width: 800, height: 600 }) 
      })
      expect(r.status).toBe(200)
    })

    it('input/window/raise', async () => {
      const r = await j('/input/window/raise', { method: 'POST', body: JSON.stringify({ match: { name: "chromium" } }) })
      expect(r.status).toBe(200)
    })

    it('input/window/minimize', async () => {
      const r = await j('/input/window/minimize', { method: 'POST', body: JSON.stringify({ match: { name: "chromium" } }) })
      expect(r.status).toBe(200)
    })

    it('input/window/map', async () => {
      const r = await j('/input/window/map', { method: 'POST', body: JSON.stringify({ match: { name: "chromium" } }) })
      expect(r.status).toBe(200)
    })

    it('input/window/unmap', async () => {
      const r = await j('/input/window/unmap', { method: 'POST', body: JSON.stringify({ match: { name: "chromium" } }) })
      expect(r.status).toBe(200)
    })

    it('input/window/close', async () => {
      // rather not close chromium - tested and works when firefox open
      const r = await j('/input/window/close', { method: 'POST', body: JSON.stringify({ match: { name: "firefox" } }) })
      expect(r.status).toBe(200)
    })

    it('input/window/active', async () => {
      const r = await j('/input/window/active', { method: 'GET' })
      expect(r.status).toBe(200)
      expect(r.body.wid).toBeDefined()
    })

    it('input/window/focused', async () => {
      const r = await j('/input/window/focused', { method: 'GET' })
      expect(r.status).toBe(200)
      expect(r.body.wid).toBeDefined()
    })

    it('input/window/name', async () => {
      // First get active window
      const active = await j('/input/window/active', { method: 'GET' })
      const r = await j('/input/window/name', { 
        method: 'POST', 
        body: JSON.stringify({ wid: active.body.wid }) 
      })
      expect(r.status).toBe(200)
      expect(r.body.name).toBeDefined()
    })

    it('input/window/geometry', async () => {
      // First get active window
      const active = await j('/input/window/active', { method: 'GET' })
      const r = await j('/input/window/geometry', { 
        method: 'POST', 
        body: JSON.stringify({ wid: active.body.wid }) 
      })
      expect(r.status).toBe(200)
      expect(r.body.width).toBeDefined()
      expect(r.body.height).toBeDefined()
    })
  })

  // Desktop endpoints
  describe('desktop', () => {
    it('input/desktop/count', async () => {
      const r = await j('/input/desktop/count', { method: 'GET' })
      expect(r.status).toBe(200)
      expect(r.body.count).toBeDefined()
    })

    it('input/desktop/current', async () => {
      const r = await j('/input/desktop/current', { method: 'GET' })
      expect(r.status).toBe(200)
      expect(r.body.index).toBeDefined()
    })

    it('input/desktop/viewport', async () => {
      const r = await j('/input/desktop/viewport', { method: 'GET' })
      expect(r.status).toBe(200)
      expect(r.body.x).toBeDefined()
      expect(r.body.y).toBeDefined()
    })
  })

  // Display endpoints
  describe('display', () => {
    it('input/display/geometry', async () => {
      const r = await j('/input/display/geometry', { method: 'GET' })
      expect(r.status).toBe(200)
      expect(r.body.width).toBeDefined()
      expect(r.body.height).toBeDefined()
    })
  })

  // Combo endpoints
  describe('combo', () => {
    it('input/combo/activate_and_type', async () => {
      const r = await j('/input/combo/activate_and_type', { 
        method: 'POST', 
        body: JSON.stringify({ match: { name: "chromium" }, text: 'test' }) 
      })
      expect(r.status).toBe(200)
    })

    it('input/combo/activate_and_keys', async () => {
      const r = await j('/input/combo/activate_and_keys', { 
        method: 'POST', 
        body: JSON.stringify({ match: { name: "chromium" }, keys: ['Return'] }) 
      })
      expect(r.status).toBe(200)
    })

    it('input/combo/window/center', async () => {
      const r = await j('/input/combo/window/center', { 
        method: 'POST', 
        body: JSON.stringify({ match: { name: "chromium" }, width: 800, height: 600 }) 
      })
      expect(r.status).toBe(200)
    })

    it('input/combo/window/snap', async () => {
      const r = await j('/input/combo/window/snap', { 
        method: 'POST', 
        body: JSON.stringify({ match: { name: "chromium" }, position: 'left' }) 
      })
      expect(r.status).toBe(200)
    })
  })

  // System endpoints
  describe('system', () => {
    it('input/system/exec', async () => {
      const r = await j('/input/system/exec', { 
        method: 'POST', 
        body: JSON.stringify({ command: 'echo', args: ['hello'] }) 
      })
      expect(r.status).toBe(200)
      expect(r.body.stdout).toBe('hello\n')
    })

    it('input/system/sleep', async () => {
      const r = await j('/input/system/sleep', { 
        method: 'POST', 
        body: JSON.stringify({ seconds: 0.1 }) 
      })
      expect(r.status).toBe(200)
      expect(r.body.ok).toBe(true)
    })
  })
})
