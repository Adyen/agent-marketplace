import { mkdir } from 'node:fs/promises'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { parseArgs } from 'node:util'
import { chromium, type Browser } from '@playwright/test'
import { createServer } from 'vite'
import { scenario } from './scenarios.ts'

const themes = ['light', 'dark'] as const
type Theme = (typeof themes)[number]

const groupByAxes = ['status', 'source', 'kind']
const thenByAxes = ['none', 'kind', 'source']

const { values } = parseArgs({
  options: {
    scenario: { type: 'string', default: 'default' },
    theme: { type: 'string', default: 'both' },
    select: { type: 'string' },
    view: { type: 'string' },
    'open-view': { type: 'boolean', default: false },
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

function parseView(value: string | undefined): string | null {
  if (value === undefined) {
    return null
  }
  const [groupBy, thenBy = 'none'] = value.split('/')
  if (!groupByAxes.includes(groupBy) || !thenByAxes.includes(thenBy) || thenBy === groupBy) {
    throw new Error(`--view must be <status|source|kind>[/<none|kind|source>] with a different Then by; got "${value}"`)
  }
  return JSON.stringify({ version: 1, groupBy, thenBy })
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
  const viewChoice = parseView(values.view)
  const context = await browser.newContext({ viewport: size, deviceScaleFactor: 1 })
  try {
    await context.addInitScript(
      ({ appearance, view }) => {
        window.localStorage.setItem('vitepress-theme-appearance', appearance)
        if (view !== null) {
          window.localStorage.setItem('am-gui-view', view)
        }
      },
      { appearance: theme, view: viewChoice },
    )
    const page = await context.newPage()
    const errors: string[] = []
    page.on('pageerror', (error) => errors.push(error.message))
    await page.goto(url)
    await page.locator('[role="option"], .gui-statusbar__synced').first().waitFor({ state: 'visible' })
    if (select !== undefined) {
      await page.locator('[role="option"]', { hasText: select }).first().click()
      await page.locator('[role="option"][aria-selected="true"]').waitFor({ state: 'visible' })
    }
    if (values['open-view']) {
      await page.locator('button[aria-label="View"]').click()
      await page.locator('.gui-view-panel').waitFor({ state: 'visible' })
    }
    await page.evaluate(() => document.fonts.ready)
    await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))))
    await page.waitForTimeout(400)
    if (errors.length > 0) {
      throw new Error(`page errors for ${name}-${theme}: ${errors.join('; ')}`)
    }
    const slug = select === undefined ? '' : `-${select.toLowerCase().replace(/[^a-z0-9]+/g, '-')}`
    const viewSlug = values.view === undefined ? '' : `-view-${values.view.replace(/\//g, '-then-')}`
    const file = join(out, `${name}-${theme}${viewSlug}${slug}${values['open-view'] ? '-view-menu' : ''}.png`)
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
