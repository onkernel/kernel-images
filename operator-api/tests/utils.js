import { execSync } from 'node:child_process'
import { request, fetch } from 'undici'
import { Readable } from 'node:stream'
import 'dotenv/config'
import chalk from 'chalk'

// Hardcoded BASE URL using PORT from environment
export const BASE = () => `http://localhost:${process.env.PORT || '10001'}`

// Debug mode constant
const DEBUG_MODE = process.env.DEBUG_LOGS === 'true' || process.env.DEBUG_LOGS === true

export function hasCmd(cmd) {
  try {
    execSync(`bash -lc "command -v ${cmd} >/dev/null 2>&1"`, { stdio: 'ignore' })
    return true
  } catch {
    return false
  }
}
export async function j(url, init = {}) {
  const fullUrl = `${BASE()}${url}`
  
  if (DEBUG_MODE) {
    // Generate curl equivalent
    let curlCmd = `curl -X ${init.method || 'GET'} "${fullUrl}"`
    
    // Add headers
    const headers = { 'content-type': 'application/json', ...(init.headers || {}) }
    Object.entries(headers).forEach(([key, value]) => {
      curlCmd += ` -H "${key}: ${value}"`
    })
    
    // Add body if present
    if (init.body) {
      curlCmd += ` -d '${init.body}'`
    }
    
    console.log(chalk.cyan('🔍 Request:'), chalk.yellow(curlCmd))
  }
  
  const r = await fetch(fullUrl, {
    ...init,
    headers: { 'content-type': 'application/json', ...(init.headers || {}) }
  })
  
  const txt = await r.text()
  let result
  
  try {
    result = { status: r.status, body: JSON.parse(txt) }
  } catch {
    // If it's binary or very large, trim the output
    const isBinary = /[\x00-\x08\x0E-\x1F]/.test(txt)
    const isLarge = txt.length > 1000
    
    if (isBinary || isLarge) {
      result = { 
        status: r.status, 
        body: `${txt.substring(0, 500)}... [${txt.length} bytes${isBinary ? ', binary content' : ''}]` 
      }
    } else {
      result = { status: r.status, body: txt }
    }
  }
  
  if (DEBUG_MODE) {
    console.log(chalk.cyan('📡 Response:'), 
      chalk.green(`Status: ${r.status}`), 
      chalk.magenta('\nBody:'), 
      typeof result.body === 'object' ? 
        chalk.yellow(JSON.stringify(result.body, null, 2)) : 
        chalk.yellow(result.body)
    )
  }
  
  return result
}

export async function raw(url, init = {}) {
  const fullUrl = `${BASE()}${url}`
  
  if (DEBUG_MODE) {
    // Generate curl equivalent
    let curlCmd = `curl -X ${init.method || 'GET'} "${fullUrl}"`
    
    // Add headers
    if (init.headers) {
      Object.entries(init.headers).forEach(([key, value]) => {
        curlCmd += ` -H "${key}: ${value}"`
      })
    }
    
    // Add body if present
    if (init.body) {
      curlCmd += ` -d '${init.body}'`
    }
    
    console.log(chalk.cyan('🔍 Raw Request:'), chalk.yellow(curlCmd))
  }
  
  const response = await fetch(fullUrl, init)
  
  if (DEBUG_MODE) {
    console.log(chalk.cyan('📡 Raw Response:'), chalk.green(`Status: ${response.status}`))
  }
  
  return response
}

// Minimal SSE reader: returns first N events parsed as JSON
export async function readSSE(path, { max = 1, timeoutMs = 5000, debug = DEBUG_MODE } = {}) {
  const fullUrl = `${BASE()}${path}`
  
  if (debug) {
    console.log(
      chalk.cyan('🔌 SSE Connection:'),
      chalk.yellow(fullUrl),
      chalk.magenta(`(max: ${max}, timeout: ${timeoutMs}ms)`)
    )
  }
  
  const res = await fetch(fullUrl)
  
  if (!res.ok) {
    if (debug) {
      console.error(
        chalk.red('❌ SSE Connection Failed:'),
        chalk.yellow(`Status: ${res.status}`)
      )
    }
    throw new Error(`bad status ${res.status}`)
  }
  
  if (debug) {
    console.log(
      chalk.green('✅ SSE Connected:'),
      chalk.yellow(`Status: ${res.status}`),
      chalk.cyan('Waiting for events...')
    )
  }
  
  const reader = res.body.getReader()
  const decoder = new TextDecoder('utf-8')
  const events = []
  let buf = ''
  const start = Date.now()

  while (events.length < max && Date.now() - start < timeoutMs) {
    const { value, done } = await reader.read()
    if (done) break
    
    const newData = decoder.decode(value, { stream: true })
    buf += newData
    
    if (debug && newData.trim()) {
      console.log(
        chalk.blue('📥 SSE Raw Data:'),
        chalk.gray(newData.replace(/\n/g, '\\n'))
      )
    }
    
    let idx
    while ((idx = buf.indexOf('\n\n')) !== -1) {
      const chunk = buf.slice(0, idx)
      buf = buf.slice(idx + 2)
      const dataLine = chunk.split('\n').find(l => l.startsWith('data: '))
      
      if (dataLine) {
        const json = dataLine.slice(6)
        try { 
          const parsedEvent = JSON.parse(json)
          events.push(parsedEvent)
          
          if (debug) {
            console.log(
              chalk.green('🎯 SSE Event Received:'),
              chalk.yellow(`[${events.length}/${max}]`),
              chalk.magenta('\nPayload:'),
              chalk.cyan(JSON.stringify(parsedEvent, null, 2))
            )
          }
        } catch (err) {
          if (debug) {
            console.error(
              chalk.red('⚠️ SSE Parse Error:'),
              chalk.yellow(json),
              chalk.red(err.message)
            )
          }
        }
      }
      
      if (events.length >= max) break
    }
  }
  
  if (debug) {
    const duration = Date.now() - start
    console.log(
      chalk.cyan('🏁 SSE Complete:'),
      chalk.yellow(`${events.length} events`),
      chalk.green(`(${duration}ms)`),
      events.length < max ? chalk.red('(timed out)') : ''
    )
  }
  
  try { reader.cancel() } catch {}
  return events
}

// helper to create random path under /tmp
export function tmpPath(name = 'kco') {
  const id = Math.random().toString(36).slice(2)
  const path = `/tmp/${name}-${id}`
  
  if (DEBUG_MODE) {
    console.log(
      chalk.cyan('📁 Temp Path:'),
      chalk.yellow(path)
    )
  }
  
  return path
}
