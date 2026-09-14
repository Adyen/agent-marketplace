import { createRequire } from 'node:module'
import { existsSync, readFileSync } from 'node:fs'
import { spawnSync } from 'node:child_process'
import { resolve } from 'node:path'

const require = createRequire(import.meta.url)
const mode = process.argv[2] || 'test'
const packageJSON = JSON.parse(readFileSync(resolve('package.json'), 'utf8'))
const playwrightDeclared = Boolean(packageJSON.dependencies?.['@playwright/test'] || packageJSON.devDependencies?.['@playwright/test'])

function fail(message) {
  console.error(`E2E failed: ${message}`)
  process.exit(1)
}

let playwrightTestPath
try {
  playwrightTestPath = require.resolve('@playwright/test')
} catch {
  if (playwrightDeclared || process.env.CI) fail('the declared Playwright test dependency is not installed.')
  console.warn('E2E skipped: the Playwright test dependency is not declared or installed for this local non-release run.')
  process.exit(0)
}

const configuredChromiumPath = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || process.env.AGENT_BROWSER_EXECUTABLE_PATH
const playwright = createRequire(playwrightTestPath)('playwright')
const chromiumPath = configuredChromiumPath || playwright.chromium.executablePath()

if (mode === 'install') {
  if (configuredChromiumPath) {
    if (!existsSync(chromiumPath)) fail(`the configured Chromium executable is unavailable at ${chromiumPath}.`)
    console.log(`Using configured Chromium executable at ${chromiumPath}.`)
    process.exit(0)
  }
  const result = spawnSync('pnpm', ['exec', 'playwright', 'install', 'chromium'], { stdio: 'inherit' })
  if (result.error || result.status !== 0) fail('Chromium could not be installed without bypassing the active package policy.')
  process.exit(0)
}

if (!existsSync(chromiumPath)) fail(`the ${configuredChromiumPath ? 'configured ' : ''}Chromium executable is unavailable at ${chromiumPath}. Set PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH or AGENT_BROWSER_EXECUTABLE_PATH to an installed Chrome/Chromium binary.`)
const build = spawnSync('pnpm', ['run', 'build'], { stdio: 'inherit' })
if (build.error || build.status !== 0) process.exit(build.status || 1)

const siteInstall = spawnSync('pnpm', ['install', '--frozen-lockfile', '--ignore-scripts'], {
  cwd: resolve('..', 'site'),
  stdio: 'inherit',
})
if (siteInstall.error || siteInstall.status !== 0) process.exit(siteInstall.status || 1)

const siteBuild = spawnSync('pnpm', ['run', 'build'], {
  cwd: resolve('..', 'site'),
  stdio: 'inherit',
})
if (siteBuild.error || siteBuild.status !== 0) process.exit(siteBuild.status || 1)

const result = spawnSync('pnpm', ['exec', 'playwright', 'test', '--config=tests/e2e/playwright.config.ts', ...process.argv.slice(3)], {
  stdio: 'inherit',
  env: { ...process.env, PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH: chromiumPath },
})
if (result.error) throw result.error
process.exit(result.status ?? 1)
