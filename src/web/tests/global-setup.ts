import { execFileSync } from 'node:child_process'
import { mkdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { e2eRunDirectory, e2eServerBinary } from './e2e-environment'

// Build the isolated test server once; every worker starts its own copy.
export default function globalSetup() {
  mkdirSync(e2eRunDirectory, { recursive: true })
  execFileSync('go', ['build', '-o', e2eServerBinary, './tests/server'], { cwd: fileURLToPath(new URL('..', import.meta.url)), stdio: 'inherit' })
}
