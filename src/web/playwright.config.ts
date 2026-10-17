import { defineConfig } from '@playwright/test'
import './tests/e2e-environment'

// Every worker starts its own isolated server and restarts it with an empty
// database for each spec file (tests/e2e.ts), so files run in parallel.
// Media-clock and layout assertions are timing-sensitive under load: keep the
// default worker count moderate and retry once; retried passes report as flaky.
// TANGENTRY_E2E_WORKERS overrides the count (1 runs serially).
const workers = process.env.TANGENTRY_E2E_WORKERS

export default defineConfig({
  testDir: './tests',
  // Keep all Playwright-owned artifacts in the ignored test output directory.
  // Product frame capture tests persist JPEG data through the app API; they do
  // not use this directory or create visual snapshots.
  outputDir: './test-results',
  preserveOutput: 'failures-only',
  globalSetup: './tests/global-setup.ts',
  globalTeardown: './tests/global-teardown.ts',
  fullyParallel: false,
  workers: workers ? (workers.endsWith('%') ? workers : Number(workers)) : 3,
  retries: 1,
  // Browser tests assert behavior and geometry without routine screenshot output.
  // Failure traces may include snapshots, but stay under ignored test-results/.
  use: { browserName: 'chromium', screenshot: 'off', video: 'off', trace: 'retain-on-failure' },
})
