import socks from 'socksv5'
import { uid } from '../utils/ids.js'

const proxies = new Map() // id -> {server, host, port}

export function startSocks({ bind_host = '127.0.0.1', bind_port = 1080, auth = null }) {
  const server = socks.createServer((info, accept, deny) => accept())
  if (auth?.username || auth?.password) {
    server.useAuth(socks.auth.UserPassword((user, pass, cb) => {
      if (user === (auth.username || '') && pass === (auth.password || '')) cb(true)
      else cb(false)
    }))
  } else {
    server.useAuth(socks.auth.None())
  }
  server.listen(bind_port, bind_host)
  const proxy_id = uid()
  proxies.set(proxy_id, { server, host: bind_host, port: bind_port })
  return { proxy_id, url: `socks5://${bind_host}:${bind_port}` }
}

export function stopSocks({ proxy_id }) {
  const p = proxies.get(proxy_id)
  if (!p) throw new Error('Not Found')
  p.server.close()
  proxies.delete(proxy_id)
  return true
}
