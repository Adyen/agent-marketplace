import { mkdir } from 'node:fs/promises'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { parseArgs } from 'node:util'
import { chromium, type Browser } from '@playwright/test'
import { createServer } from 'vite'
import { scenario } from './scenarios.ts'

const themes = ['light', 'dark'] as const
type Theme = (typeof themes)[number]

const { values } = parseArgs({
  options: {
    scenario: { type: 'string', default: 'default' },
    theme: { type: 'string', default: 'both' },
    select: { type: 'string' },
    size: { type: 'string', default: '1280x832' },
    out: { type: 'string', default: fileURLToPath(new URL('../test-results/preview', import.meta.url)) },
  },
})

function parseThemes(value: string): Theme[] {
  if (value === 'both') {
    return [...themes]
  }
  if (value === 'light' || value === 'dark') {
    return [value]
  }
  throw new Error(`--theme must be light, dark, or both; got "${value}"`)
}

function parseSize(value: string): { width: number; height: number } {
  const match = /^(\d+)x(\d+)$/.exec(value)
  if (match === null) {
    throw new Error(`--size must be WxH; got "${value}"`)
  }
  return { width: Number(match[1]), height: Number(match[2]) }
}

async function capture(browser: Browser, url: string, theme: Theme, size: { width: number; height: number }): Promise<string> {
  const { scenario: name, select, out } = values
  const context = await browser.newContext({ viewport: size, deviceScaleFactor: 1 })
  try {
    await context.addInitScript((appearance) => window.localStorage.setItem('vitepress-theme-appearance', appearance), theme)
    const page = await context.newPage()
    const errors: string[] = []
    page.on('pageerror', (error) => errors.push(error.message))
    await page.goto(url)
    await page.locator('[role="option"], .gui-statusbar__synced').first().waitFor({ state: 'visible' })
    if (select !== undefined) {
      await page.locator('[role="option"]', { hasText: select }).first().click()
      await page.locator('[role="option"][aria-selected="true"]').waitFor({ state: 'visible' })
    }
    await page.evaluate(() => document.fonts.ready)
    await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))))
    if (errors.length > 0) {
      throw new Error(`page errors for ${name}-${theme}: ${errors.join('; ')}`)
    }
    const slug = select === undefined ? '' : `-${select.toLowerCase().replace(/[^a-z0-9]+/g, '-')}`
    const file = join(out, `${name}-${theme}${slug}.png`)
    await page.screenshot({ path: file })
    return file
  } finally {
    await context.close()
  }
}

async function main() {
  scenario(values.scenario)
  const themeList = parseThemes(values.theme)
  const size = parseSize(values.size)
  process.env.GUI_PREVIEW_SCENARIO = values.scenario

  const server = await createServer({
    configFile: fileURLToPath(new URL('../vite.preview.config.ts', import.meta.url)),
    server: { port: 0, strictPort: false },
  })
  const browser = await chromium.launch()
  try {
    await server.listen()
    const url = server.resolvedUrls?.local[0]
    if (url === undefined) {
      throw new Error('vite did not report a local URL')
    }
    await mkdir(values.out, { recursive: true })
    for (const theme of themeList) {
      console.log(await capture(browser, url, theme, size))
    }
  } finally {
    await browser.close()
    await server.close()
  }
}

main().catch((error: unknown) => {
  console.error(error instanceof Error ? error.message : error)
  process.exitCode = 1
})
