import { readdirSync, rmSync, rmdirSync } from 'node:fs'
import { e2eRunDirectory, e2eServerBinary } from './e2e-environment'

// Remove the run directory unless a failed file kept its data for debugging.
export default function globalTeardown() {
  rmSync(e2eServerBinary, { force: true })
  for (const worker of readdirSync(e2eRunDirectory)) {
    try { rmdirSync(`${e2eRunDirectory}/${worker}`) } catch { /* not empty: failed data stays */ }
  }
  try { rmdirSync(e2eRunDirectory) } catch { /* not empty: failed data stays */ }
}
