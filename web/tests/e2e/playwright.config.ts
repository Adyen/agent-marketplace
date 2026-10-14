import { defineConfig } from '@playwright/test'

const mockPort = process.env.E2E_MOCK_PORT || '4311'
const appPort = process.env.E2E_APP_PORT || '4321'
const relativeAppPort = process.env.E2E_RELATIVE_APP_PORT || '4322'
const sharedOriginPort = process.env.E2E_SHARED_ORIGIN_PORT || '4323'
const sitePort = process.env.E2E_SITE_PORT || '4324'
const rootAppPort = process.env.E2E_ROOT_APP_PORT || '4325'
const publicSitePort = process.env.E2E_PUBLIC_SITE_PORT || '4326'
const mockURL = `http://127.0.0.1:${mockPort}`
const appURL = `http://127.0.0.1:${appPort}`
const relativeAppURL = `http://127.0.0.1:${relativeAppPort}`
const relativeAppBaseURL = `${relativeAppURL}/marketplace/`
const sharedOriginURL = `http://127.0.0.1:${sharedOriginPort}`
const siteURL = `http://127.0.0.1:${sitePort}`
const rootAppURL = `http://127.0.0.1:${rootAppPort}`
const publicSiteURL = `http://127.0.0.1:${publicSitePort}`
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
    {
      command: 'node .output/server/index.mjs',
      cwd: '../..',
      url: rootAppURL,
      reuseExistingServer: false,
      env: { ...process.env, HOST: '127.0.0.1', PORT: rootAppPort, NUXT_PUBLIC_API_BASE: '/', NUXT_API_BASE_INTERNAL: mockURL },
    },
    {
      command: `pnpm run preview --host 127.0.0.1 --port ${publicSitePort}`,
      cwd: '../../../site',
      url: publicSiteURL,
      reuseExistingServer: false,
      env: { ...process.env, SITE_PROFILE: 'public' },
    },
    {
      command: `pnpm run preview --host 127.0.0.1 --port ${sitePort}`,
      cwd: '../../../site',
      url: `${siteURL}/docs/`,
      reuseExistingServer: false,
      env: { ...process.env, SITE_PROFILE: 'docs' },
    },
    {
      command: 'node shared-origin-proxy.mjs',
      url: sharedOriginURL,
      reuseExistingServer: false,
      env: {
        ...process.env,
        E2E_SHARED_ORIGIN_PORT: sharedOriginPort,
        E2E_SITE_ORIGIN: siteURL,
        E2E_ROOT_APP_ORIGIN: rootAppURL,
        E2E_API_ORIGIN: mockURL,
      },
    },
  ],
})
