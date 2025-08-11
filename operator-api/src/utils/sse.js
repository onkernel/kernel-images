export function sseHeaders(extra = {}) {
  return {
    'Content-Type': 'text/event-stream; charset=utf-8',
    'Cache-Control': 'no-cache, no-transform',
    Connection: 'keep-alive',
    'X-SSE-Content-Type': 'application/json',
    ...extra
  }
}

export function sseFormat(obj) {
  return `event: data\ndata: ${JSON.stringify(obj)}\n\n`
}
