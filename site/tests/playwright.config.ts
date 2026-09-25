import { defineConfig } from '@playwright/test'

const base = process.env.SITE_BASE ?? '/'

export default defineConfig({
  testDir: '.',
  testMatch: 'docs.spec.ts',
  outputDir: '../.vitepress/cache/test-results',
  fullyParallel: true,
  workers: 2,
  retries: 0,
  use: {
    baseURL: `http://127.0.0.1:4173${base}`,
    launchOptions: {
      executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || process.env.AGENT_BROWSER_EXECUTABLE_PATH
    },
    trace: 'retain-on-failure'
  },
  webServer: {
    command: 'node node_modules/vitepress/bin/vitepress.js preview --host 127.0.0.1 --port 4173',
    cwd: '..',
    url: `http://127.0.0.1:4173${base}`,
    reuseExistingServer: false
  }
})
