import { defineConfig } from '@playwright/test'

const mockPort = process.env.E2E_MOCK_PORT || '4311'
const appPort = process.env.E2E_APP_PORT || '4321'
const relativeAppPort = process.env.E2E_RELATIVE_APP_PORT || '4322'
const mockURL = `http://127.0.0.1:${mockPort}`
const appURL = `http://127.0.0.1:${appPort}`
const relativeAppURL = `http://127.0.0.1:${relativeAppPort}`
const relativeAppBaseURL = `${relativeAppURL}/marketplace/`
const chromiumExecutablePath = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || process.env.AGENT_BROWSER_EXECUTABLE_PATH

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
    launchOptions: chromiumExecutablePath ? { executablePath: chromiumExecutablePath } : undefined,
  },
  webServer: [
    {
      command: 'node mock-marketplace.mjs',
      url: `${mockURL}/.well-known/agent-marketplace.json`,
      reuseExistingServer: false,
      env: { ...process.env, E2E_APP_ORIGIN: appURL },
    },
    {
      command: 'node .output/server/index.mjs',
      cwd: '../..',
      url: appURL,
      reuseExistingServer: false,
      env: { ...process.env, HOST: '127.0.0.1', PORT: appPort, NUXT_PUBLIC_API_BASE: mockURL, NUXT_API_BASE_INTERNAL: '' },
    },
    {
      command: 'node .output/server/index.mjs',
      cwd: '../..',
      url: relativeAppBaseURL,
      reuseExistingServer: false,
      env: { ...process.env, HOST: '127.0.0.1', PORT: relativeAppPort, NUXT_APP_BASE_URL: '/marketplace/', NUXT_PUBLIC_API_BASE: '/', NUXT_API_BASE_INTERNAL: mockURL },
    },
  ],
})
