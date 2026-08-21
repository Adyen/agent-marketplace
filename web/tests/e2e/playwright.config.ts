import { defineConfig } from '@playwright/test'

const mockPort = process.env.E2E_MOCK_PORT || '4311'
const appPort = process.env.E2E_APP_PORT || '4321'
const relativeAppPort = process.env.E2E_RELATIVE_APP_PORT || '4322'
const mockURL = `http://127.0.0.1:${mockPort}`
const appURL = `http://127.0.0.1:${appPort}`
const relativeAppURL = `http://127.0.0.1:${relativeAppPort}`

export default defineConfig({
  testDir: '.',
  testMatch: '**/*.spec.ts',
  fullyParallel: false,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 2 : 0,
  reporter: process.env.CI ? 'github' : 'list',
  use: {
    baseURL: appURL,
    trace: 'retain-on-failure',
  },
  webServer: [
    {
      command: 'node tests/e2e/mock-marketplace.mjs',
      url: `${mockURL}/.well-known/agent-marketplace.json`,
      reuseExistingServer: !process.env.CI,
      env: { ...process.env, E2E_APP_ORIGIN: appURL },
    },
    {
      command: `pnpm exec nuxt preview --host 127.0.0.1 --port ${appPort}`,
      url: appURL,
      reuseExistingServer: !process.env.CI,
      env: { ...process.env, NUXT_PUBLIC_API_BASE: mockURL, NUXT_API_BASE_INTERNAL: '' },
    },
    {
      command: `pnpm exec nuxt preview --host 127.0.0.1 --port ${relativeAppPort}`,
      url: relativeAppURL,
      reuseExistingServer: !process.env.CI,
      env: { ...process.env, NUXT_PUBLIC_API_BASE: '/', NUXT_API_BASE_INTERNAL: mockURL },
    },
  ],
})
