import { test as base } from '@playwright/test'
import { spawn, type ChildProcess } from 'node:child_process'
import { mkdirSync, openSync, rmSync } from 'node:fs'
import { e2eRunDirectory, e2eServerBinary, e2eServerEnv } from './e2e-environment'

export * from '@playwright/test'

const basePort = Number(process.env.TANGENTRY_E2E_PORT || 7391)

// Each worker owns one isolated Go server on its own port. The server restarts
// with an empty SQLite database whenever the worker moves to another spec file,
// so files never see each other's data and can run in parallel in any order.
// Tests within one file share the database, as they always have, except
// viewing and search history: those lived in each test's fresh browser
// storage before moving to the database, so every test starts without them.
class E2EServer {
  private process?: ChildProcess
  private exited?: Promise<void>
  private file = ''
  private data = ''
  private failed = false
  private generation = 0

  constructor(private readonly worker: number, readonly url = `http://127.0.0.1:${basePort + worker}`) {}

  async serve(file: string) {
    if (file === this.file && this.process) return
    await this.stop()
    this.file = file
    const data = `${e2eRunDirectory}/worker-${this.worker}/${++this.generation}`
    this.data = data
    this.failed = false
    mkdirSync(data, { recursive: true })
    const log = openSync(`${data}/server.log`, 'a')
    const server = spawn(e2eServerBinary, ['-port', String(basePort + this.worker), '-data', data], { env: e2eServerEnv(data), stdio: ['ignore', log, log] })
    let stopped = false
    this.process = server
    this.exited = new Promise<void>(resolve => server.once('exit', () => { stopped = true; resolve() }))
    const deadline = Date.now() + 60_000
    while (Date.now() < deadline) {
      if (stopped) throw new Error(`E2E server exited during startup; see ${data}/server.log`)
      try {
        if ((await fetch(`${this.url}/api/health`)).ok) return
      } catch { /* not listening yet */ }
      await new Promise(resolve => setTimeout(resolve, 50))
    }
    throw new Error(`E2E server did not become healthy; see ${data}/server.log`)
  }

  markFailed() { this.failed = true }

  // A file's database and server log are kept only when one of its tests failed.
  async stop() {
    if (!this.process) return
    this.process.kill('SIGTERM')
    await this.exited
    this.process = undefined
    if (!this.failed) rmSync(this.data, { recursive: true, force: true })
  }
}

export const test = base.extend<{ e2eFile: void }, { e2eServer: E2EServer }>({
  e2eServer: [async ({}, use, workerInfo) => {
    const server = new E2EServer(workerInfo.workerIndex)
    await use(server)
    await server.stop()
  }, { scope: 'worker', timeout: 90_000 }],
  e2eFile: [async ({ e2eServer }, use, testInfo) => {
    await e2eServer.serve(testInfo.file)
    for (const kind of ['viewing', 'searches']) {
      const response = await fetch(`${e2eServer.url}/api/history/${kind}`, { method: 'DELETE' })
      if (!response.ok) throw new Error(`could not clear ${kind} history: ${response.status}`)
    }
    await use()
    if (testInfo.status !== testInfo.expectedStatus) e2eServer.markFailed()
  }, { auto: true }],
  baseURL: async ({ e2eServer }, use) => use(e2eServer.url),
})
