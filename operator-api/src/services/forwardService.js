import { spawn } from 'node:child_process'
import { uid } from '../utils/ids.js'

const forwards = new Map() // id -> {proc, direction, host_port, vm_port}

export function addForward({ direction, host_port, vm_port }) {
  const id = uid()
  let proc
  if (direction === 'host_to_vm') {
    // Listen on VM host_port and forward to vm_port (localhost)
    proc = spawn('socat', [`TCP-LISTEN:${host_port},fork,reuseaddr`, `TCP:127.0.0.1:${vm_port}`])
  } else {
    proc = spawn('socat', [`TCP-LISTEN:${vm_port},fork,reuseaddr`, `TCP:127.0.0.1:${host_port}`])
  }
  forwards.set(id, { proc, direction, host_port, vm_port })
  return { forward_id: id, active: true }
}

export function removeForward({ forward_id }) {
  const item = forwards.get(forward_id)
  if (!item) throw new Error('Not Found')
  item.proc.kill('SIGINT')
  forwards.delete(forward_id)
  return true
}
