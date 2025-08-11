import 'dotenv/config'
import http from 'node:http'
import httpProxy from 'http-proxy'
import { uid } from '../utils/ids.js'
import { EventEmitter } from 'node:events'

const ruleSets = new Map() // id -> {rules}
const harEmitter = new EventEmitter()

let proxyServer = null
let runningPort = Number(process.env.HTTP_PROXY_PORT || 8082)

function ensureProxy() {
  if (proxyServer) return
  const proxy = httpProxy.createProxyServer({})
  proxy.on('proxyRes', function (proxyRes, req, res) {
    const entry = {
      request: { method: req.method, url: req.url, headers: Object.entries(req.headers).map(([name, value]) => ({ name, value: String(value) })) },
      response: { status: proxyRes.statusCode, headers: Object.entries(proxyRes.headers).map(([name, value]) => ({ name, value: String(value) })) }
    }
    harEmitter.emit('har', { ts: new Date().toISOString(), entry })
  })
  proxyServer = http.createServer((req, res) => {
    const url = new URL(req.url, `http://${req.headers.host}`)
    // apply first matching rule
    for (const [, set] of ruleSets) {
      for (const rule of set.rules) {
        const m = rule.match || {}
        const methodOk = !m.method || m.method === req.method
        const hostOk = !m.host_contains || (req.headers.host || '').includes(m.host_contains)
        const pathOk = !m.path_regex || new RegExp(m.path_regex).test(url.pathname)
        const protoOk = !m.protocol || m.protocol === 'http'
        if (methodOk && hostOk && pathOk && protoOk) {
          const a = rule.action
          if (a.type === 'block') {
            res.statusCode = 403
            res.end('blocked')
            return
          }
          if (a.type === 'delay' && a.delay_ms) {
            const hold = setTimeout(() => {}, a.delay_ms)
          }
          if (a.type === 'mock_response') {
            res.writeHead(a.status || 200, a.set_response_headers || {})
            const body = a.body_b64 ? Buffer.from(a.body_b64, 'base64') : Buffer.from('')
            res.end(body)
            return
          }
          if (a.type === 'modify_request' && a.set_request_headers) {
            for (const [k, v] of Object.entries(a.set_request_headers)) req.headers[k.toLowerCase()] = v
          }
        }
      }
    }
    proxy.web(req, res, { target: `${url.protocol}//${url.host}` })
  })
  proxyServer.listen(runningPort, '0.0.0.0')
}

export function applyRules({ rules }) {
  ensureProxy()
  const id = uid()
  ruleSets.set(id, { rules })
  return { rule_set_id: id, applied: true }
}

export function deleteRuleSet({ rule_set_id }) {
  if (!ruleSets.has(rule_set_id)) throw new Error('Not Found')
  ruleSets.delete(rule_set_id)
  return true
}

export function harStreamEmitter() {
  ensureProxy()
  return harEmitter
}
