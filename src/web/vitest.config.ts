import { defineConfig } from 'vitest/config'

// Unit tests sit beside the code they cover; Playwright owns tests/.
export default defineConfig({
  test: { include: ['src/**/*.test.{ts,tsx}'], environment: 'node' },
})
