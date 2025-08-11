import 'dotenv/config'
import fs from 'node:fs'
import path from 'node:path'

export const DATA_DIR = process.env.DATA_DIR || '/tmp/kernel-operator-api/data'
export const TMP_DIR = path.join(DATA_DIR, 'tmp')
export const SCRIPTS_DIR = path.join(DATA_DIR, 'scripts')
export const RECORDINGS_DIR = path.join(DATA_DIR, 'recordings')
export const SCREENSHOTS_DIR = path.join(DATA_DIR, 'screenshots')

export function ensureDirs() {
  for (const p of [DATA_DIR, TMP_DIR, SCRIPTS_DIR, RECORDINGS_DIR, SCREENSHOTS_DIR]) {
    fs.mkdirSync(p, { recursive: true })
  }
}

ensureDirs()
