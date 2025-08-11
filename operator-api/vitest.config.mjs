import { defineConfig } from 'vitest/config'

export default defineConfig({
  test: {
    globals: true,
    environment: 'node',
    include: ['tests/**/*.test.js'],
    globalSetup: './tests/globalSetup.mjs',
    hookTimeout: 30000,
    testTimeout: 30000,
    bail: 0
  }
})
