import { spawn } from 'node:child_process'
import chalk from 'chalk'

const DEBUG_ENABLED = process.env.DEBUG_LOGS === 'true' || process.env.DEBUG_LOGS === true

function debug(...args) {
  if (DEBUG_ENABLED) {
    console.log(...args)
  }
}
export function execSpawn(cmd, args = [], opts = {}) {
  debug(chalk.blue('[EXEC]'), chalk.cyan('Spawning command:'), chalk.yellow(`${cmd} ${args.join(' ')}`))
  const child = spawn(cmd, args, { shell: false, ...opts })
  
  if (DEBUG_ENABLED) {
    child.stdout?.on('data', (data) => {
      debug(chalk.blue('[EXEC]'), chalk.green('[STDOUT]'), data.toString().trim())
    })
    
    child.stderr?.on('data', (data) => {
      debug(chalk.blue('[EXEC]'), chalk.red('[STDERR]'), data.toString().trim())
    })
    
    child.on('close', (code) => {
      const exitColor = code === 0 ? chalk.green : chalk.red
      debug(chalk.blue('[EXEC]'), chalk.cyan('Command exited with code:'), exitColor(code))
    })
  }
  
  return child
}

export async function execCapture(cmd, args = [], opts = {}) {
  debug(chalk.blue('[EXEC]'), chalk.cyan('Capturing output from:'), chalk.yellow(`${cmd} ${args.join(' ')}`))
  return new Promise((resolve) => {
    const child = execSpawn(cmd, args, opts)
    let stdout = Buffer.alloc(0)
    let stderr = Buffer.alloc(0)
    
    child.stdout?.on('data', (d) => {
      debug(chalk.blue('[EXEC]'), chalk.green('[STDOUT]'), chalk.cyan(`Received ${d.length} bytes`))
      stdout = Buffer.concat([stdout, d])
    })
    
    child.stderr?.on('data', (d) => {
      const stderrStr = d.toString().trim()
      debug(
        chalk.blue('[EXEC]'), 
        chalk.red('[STDERR]'), 
        chalk.cyan(`Received ${d.length} bytes`),
        '\n', 
        chalk.red('↳'), 
        chalk.yellow(stderrStr)
      )
      stderr = Buffer.concat([stderr, d])
    })
    
    child.on('close', (code) => {
      const exitColor = code === 0 ? chalk.green : chalk.red
      debug(
        chalk.blue('[EXEC]'),
        chalk.cyan('Command completed with exit code:'),
        exitColor(code)
      )
      
      debug(
        chalk.blue('[EXEC]'),
        chalk.cyan(`Total stdout: ${stdout.length} bytes, stderr: ${stderr.length} bytes`)
      )
      
      if (stdout.length > 0) {
        const preview = stdout.toString().substring(0, 100)
        debug(
          chalk.blue('[EXEC]'),
          chalk.green('[STDOUT]'),
          chalk.cyan('Preview:'),
          '\n',
          chalk.green('↳'),
          preview + (stdout.length > 100 ? chalk.dim('...') : '')
        )
      }
      
      if (stderr.length > 0) {
        const preview = stderr.toString().substring(0, 100)
        debug(
          chalk.blue('[EXEC]'),
          chalk.red('[STDERR]'),
          chalk.cyan('Preview:'),
          '\n',
          chalk.red('↳'),
          preview + (stderr.length > 100 ? chalk.dim('...') : '')
        )
      }
      
      resolve({ code, stdout, stderr })
    })
  })
}
