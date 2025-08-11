import { j, raw } from './utils.js'

describe('macros', () => {
  it('create/list/delete macro', async () => {
    const create = await j('/macros/create', {
      method: 'POST',
      body: JSON.stringify({
        name: 'type-hello',
        steps: [{ action: 'sleep', ms: 10 }, { action: 'keyboard.type', text: 'hello' }]
      })
    })
    expect(create.status).toBe(200)
    const macro_id = create.body.macro_id

    const list = await j('/macros/list')
    expect(list.status).toBe(200)
    expect(list.body.items.find(i => i.macro_id === macro_id)).toBeTruthy()

    const del = await j(`/macros/${macro_id}`, { method: 'DELETE' })
    expect(del.status).toBe(200)
  })

  it('handles macro run requests', async () => {
    // Create a test macro first
    const create = await j('/macros/create', {
      method: 'POST',
      body: JSON.stringify({
        name: 'test-run-macro',
        steps: [{ action: 'sleep', ms: 5 }]
      })
    })
    expect(create.status).toBe(200)
    const macro_id = create.body.macro_id

    // Test run endpoint
    const run = await j('/macros/run', {
      method: 'POST',
      body: JSON.stringify({
        macro_id
      })
    })
    expect(run.status).toBe(200)

    // Test invalid run request
    const invalidRun = await j('/macros/run', {
      method: 'POST',
      body: JSON.stringify({
        macro_id: 'non-existent-id'
      })
    })
    expect(invalidRun.status).toBe(404)

    // Clean up
    await j(`/macros/${macro_id}`, { method: 'DELETE' })
  })

})
