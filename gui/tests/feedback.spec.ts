import { expect, test, type Locator, type Page } from '@playwright/test'
import { startHarness } from './harness'
import { distIndexExists, launchBuiltUserApp } from './app'
import { fileURLToPath } from 'node:url'
import { createServer, type ViteDevServer } from 'vite'

let previewServer: ViteDevServer
let previewURL = ''
let previousScenario: string | undefined

test.beforeAll(async () => {
  previousScenario = process.env.GUI_PREVIEW_SCENARIO
  process.env.GUI_PREVIEW_SCENARIO = 'default'
  previewServer = await createServer({
    configFile: fileURLToPath(new URL('../vite.preview.config.ts', import.meta.url)),
    server: { port: 0, strictPort: false },
  })
  await previewServer.listen()
  previewURL = previewServer.resolvedUrls?.local[0] ?? ''
  if (previewURL === '') {
    throw new Error('Vite preview did not report a local URL')
  }
})

test.afterAll(async () => {
  await previewServer?.close()
  if (previousScenario === undefined) {
    delete process.env.GUI_PREVIEW_SCENARIO
  } else {
    process.env.GUI_PREVIEW_SCENARIO = previousScenario
  }
})

async function openPreview(page: Page) {
  await page.setViewportSize({ width: 1440, height: 380 })
  await page.addInitScript(() => {
    if (localStorage.getItem('vitepress-theme-appearance') == null) {
      localStorage.setItem('vitepress-theme-appearance', 'light')
    }
    localStorage.setItem('am-gui-view', JSON.stringify({ version: 1, groupBy: 'status', thenBy: 'kind' }))
  })
  await page.goto(previewURL)
  await expect(page.getByRole('option').first()).toBeVisible()
}

async function seedLightThemeOnce(page: Page) {
  await page.addInitScript(() => {
    if (localStorage.getItem('vitepress-theme-appearance') === null) {
      localStorage.setItem('vitepress-theme-appearance', 'light')
    }
  })
}

// spec: gui-shell/long-load-failure-warning-stays-inside-its-row
test('long resource warnings stay inside the row in both themes', async ({ page }) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run `make gui-build` first')
  }
  const harness = await startHarness('user', { AM_LOCALAPI_USER_FIXTURE: 'long-warning' })
  const { bootstrap } = harness
  try {
    await page.setViewportSize({ width: 1024, height: 720 })
    await seedLightThemeOnce(page)
    await launchBuiltUserApp(page, bootstrap)
    const row = page.getByRole('option').filter({ hasText: 'broken' })
    await expect(row).toBeVisible({ timeout: 60_000 })
    const warning = row.locator('.workspace__reason')
    await expect(warning).toBeVisible()
    await expect(warning).toHaveText(/.{40}/)

    for (const theme of ['light', 'dark']) {
      if (theme === 'dark') {
        await page.evaluate(() => localStorage.setItem('vitepress-theme-appearance', 'dark'))
        await page.reload()
        await expect(page.locator('html')).toHaveClass(/(?:^|\s)dark(?:\s|$)/)
        await expect(page.getByRole('option').filter({ hasText: 'broken' })).toBeVisible({ timeout: 60_000 })
      }
      const metrics = await row.evaluate((element) => {
        const list = element.closest('.workspace__list')
        const warning = element.querySelector('.workspace__reason')
        const registry = element.querySelector('.workspace__registry')
        if (list == null || warning == null || registry == null) {
          throw new Error('resource row layout is incomplete')
        }
        return {
          scrollWidth: list.scrollWidth,
          clientWidth: list.clientWidth,
          warningRight: warning.getBoundingClientRect().right,
          sourceLeft: registry.getBoundingClientRect().left,
        }
      })
      expect(metrics.scrollWidth, `${theme} resources list should not scroll horizontally`).toBeLessThanOrEqual(metrics.clientWidth)
      expect(metrics.warningRight, `${theme} warning should end before the source column`).toBeLessThanOrEqual(metrics.sourceLeft)
    }
  } finally {
    await harness.stop()
  }
})

// spec: gui-shell/source-tooltip-is-readable-in-both-themes
test('source tooltips show the full location with readable contrast in both themes', async ({ page }) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run `make gui-build` first')
  }
  const harness = await startHarness('user', { AM_LOCALAPI_USER_FIXTURE: 'sources' })
  const { bootstrap } = harness
  try {
    const location = `${bootstrap.home}/keep-source-with-a-deliberately-long-directory-name-that-wraps-onto-two-lines`
    await seedLightThemeOnce(page)
    await launchBuiltUserApp(page, bootstrap)
    const row = page.getByRole('button', { name: `Open source ${location}` })
    const tooltip = page.locator('.gui-source-row__tooltip')
    const tooltipText = tooltip.locator('[data-slot="text"]')

    for (const theme of ['light', 'dark']) {
      if (theme === 'dark') {
        await page.evaluate(() => localStorage.setItem('vitepress-theme-appearance', 'dark'))
        await page.reload()
        await expect(page.locator('html')).toHaveClass(/(?:^|\s)dark(?:\s|$)/)
      }
      await expect(row).toBeVisible({ timeout: 60_000 })
      await row.hover()
      await expect(tooltip).toBeVisible()
      await expect(tooltipText).toHaveText(location)
      await page.mouse.move(0, 0)
      await row.focus()
      await expect(tooltip).toBeVisible()
      await expect(tooltipText).toHaveText(location)
      const ratio = await tooltip.evaluate((element) => {
        const rgb = (value: string) => {
          const channels = value.match(/[\d.]+/g)?.slice(0, 3).map(Number)
          if (channels == null || channels.length !== 3) {
            throw new Error(`cannot parse computed color ${value}`)
          }
          return channels.map((channel) => channel / 255)
        }
        const luminance = (color: number[]) => color
          .map((channel) => channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4)
          .reduce((sum, channel, index) => sum + channel * [0.2126, 0.7152, 0.0722][index]!, 0)
        const style = getComputedStyle(element)
        const foreground = luminance(rgb(style.color))
        const background = luminance(rgb(style.backgroundColor))
        return (Math.max(foreground, background) + 0.05) / (Math.min(foreground, background) + 0.05)
      })
      expect(ratio, `${theme} tooltip contrast`).toBeGreaterThanOrEqual(4.5)
    }
  } finally {
    await harness.stop()
  }
})

// spec: gui-shell/source-tooltip-is-readable-in-both-themes
test('source tooltip of the last row in a scrolled source list stays inside the viewport', async ({ page }) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run `make gui-build` first')
  }
  const harness = await startHarness('user', { AM_LOCALAPI_USER_FIXTURE: 'sources' })
  const { bootstrap } = harness
  try {
    await page.setViewportSize({ width: 1024, height: 360 })
    await seedLightThemeOnce(page)
    await launchBuiltUserApp(page, bootstrap)
    const list = page.locator('.gui-sidebar__list--scroll')
    const lastRow = list.locator('button[aria-label^="Open source "]').last()
    await expect(lastRow).toBeAttached({ timeout: 60_000 })
    await expect.poll(() => list.evaluate((element) => element.scrollHeight > element.clientHeight)).toBe(true)
    await list.evaluate((element) => {
      element.scrollTop = element.scrollHeight
    })

    await lastRow.hover()
    const tooltip = page.locator('.gui-source-row__tooltip')
    await expect(tooltip).toBeVisible()
    const box = await tooltip.boundingBox()
    const viewport = page.viewportSize()!
    expect(box, 'tooltip has a layout box').not.toBeNull()
    expect(box!.x).toBeGreaterThanOrEqual(0)
    expect(box!.y).toBeGreaterThanOrEqual(0)
    expect(box!.x + box!.width).toBeLessThanOrEqual(viewport.width)
    expect(box!.y + box!.height).toBeLessThanOrEqual(viewport.height)
  } finally {
    await harness.stop()
  }
})

test('Then-by status headers stay opaque and flush with the resource list while scrolling', async ({ page }) => {
  await openPreview(page)
  const list = page.locator('.workspace__list')
  const headerAtTop = async (label: string) => page.locator('.workspace__list h2').filter({
    has: page.locator(`button[aria-label="${label}"]`),
  }).evaluate((heading) => {
    const listElement = heading.closest('.workspace__list') as HTMLElement
    const rect = heading.getBoundingClientRect()
    const listRect = listElement.getBoundingClientRect()
    const background = getComputedStyle(heading).backgroundColor
    const channels = background.match(/[\d.]+/g) ?? []
    const sample = document.elementFromPoint(listRect.left + 100, listRect.top + 5)
    return {
      top: rect.top,
      listTop: listRect.top,
      backgroundAlpha: channels.length === 4 ? Number(channels[3]) : 1,
      currentHeader: sample?.closest('.workspace__group-toggle')?.getAttribute('aria-label'),
      rowAtHeader: sample?.closest('[role="option"]') != null,
      scrollTop: listElement.scrollTop,
    }
  })

  for (const theme of ['light', 'dark']) {
    if (theme === 'dark') {
      await page.evaluate(() => localStorage.setItem('vitepress-theme-appearance', 'dark'))
      await page.reload()
      await expect(page.locator('html')).toHaveClass(/(?:^|\s)dark(?:\s|$)/)
    }
    await list.evaluate((element) => { element.scrollTop = 200 })
    const enabled = await headerAtTop('Enabled')
    expect(enabled.scrollTop).toBeGreaterThan(100)
    expect(Math.abs(enabled.top - enabled.listTop), `${theme} Enabled header top`).toBeLessThanOrEqual(1)
    expect(enabled.backgroundAlpha, `${theme} Enabled header background`).toBe(1)
    expect(enabled.currentHeader).toBe('Enabled')
    expect(enabled.rowAtHeader).toBe(false)

    const pendingOffset = await page.locator('.workspace__list h2').filter({
      has: page.locator('button[aria-label="Pending sync"]'),
    }).evaluate((heading) => {
      const list = heading.closest('.workspace__list') as HTMLElement
      return heading.getBoundingClientRect().top - list.getBoundingClientRect().top + list.scrollTop
    })
    await list.evaluate((element, offset) => { element.scrollTop = offset + 24 }, pendingOffset)
    const pending = await headerAtTop('Pending sync')
    expect(pending.scrollTop).toBeGreaterThan(100)
    expect(Math.abs(pending.top - pending.listTop), `${theme} Pending sync header top`).toBeLessThanOrEqual(1)
    expect(pending.backgroundAlpha, `${theme} Pending sync header background`).toBe(1)
    expect(pending.currentHeader).toBe('Pending sync')
    expect(pending.rowAtHeader).toBe(false)
  }
})

test('Resources and Diagnostics share a visible active navigation style', async ({ page }) => {
  await openPreview(page)
  const resources = page.getByRole('button', { name: 'Show all groups' })
  const diagnostics = page.getByRole('button', { name: 'Show diagnostics' })
  const background = (locator: Locator) => locator.evaluate((element) => getComputedStyle(element).backgroundColor)

  for (const theme of ['light', 'dark']) {
    if (theme === 'dark') {
      await page.evaluate(() => localStorage.setItem('vitepress-theme-appearance', 'dark'))
      await page.reload()
      await expect(page.locator('html')).toHaveClass(/(?:^|\s)dark(?:\s|$)/)
    }
    await expect(resources).toHaveAttribute('aria-current', 'page')
    expect(await background(resources), `${theme} active Resources row`).not.toBe('rgba(0, 0, 0, 0)')
    await diagnostics.click()
    await expect(diagnostics).toHaveAttribute('aria-current', 'page')
    await expect(resources).not.toHaveAttribute('aria-current')
    await page.waitForTimeout(200)
    expect(await background(diagnostics), `${theme} active Diagnostics row`).not.toBe('rgba(0, 0, 0, 0)')
    expect(await background(resources), `${theme} inactive Resources row`).toBe('rgba(0, 0, 0, 0)')
    await resources.click()
    await page.waitForTimeout(200)
  }
})

test('diagnostic messages are selectable apart from their keyboard-operable expand control', async ({ page }) => {
  await openPreview(page)
  await page.getByRole('button', { name: 'Show diagnostics' }).click()
  const list = page.locator('.gui-diagnostics-view ul[aria-label="Diagnostics"]')
  const message = list.locator('.gui-diagnostics__message').first()
  await expect(message).toBeVisible()
  expect(await message.evaluate((element) => element.closest('button'))).toBeNull()

  const expand = page.locator('.gui-diagnostics__expand').first()
  await expect(expand).toHaveAccessibleName(/^Expand /)
  await expect(expand).toHaveAttribute('aria-expanded', 'false')
  await expand.focus()
  await page.keyboard.press('Enter')
  await expect(expand).toHaveAttribute('aria-expanded', 'true')
  await expect(list.locator('.gui-diagnostics__messages li').first()).toBeVisible()
  await page.keyboard.press('Space')
  await expect(expand).toHaveAttribute('aria-expanded', 'false')

  const box = await message.boundingBox()
  if (box == null) {
    throw new Error('diagnostic message has no pointer-selectable geometry')
  }
  await page.mouse.move(box.x + 2, box.y + 8)
  await page.mouse.down()
  await page.mouse.move(box.x + 60, box.y + 8, { steps: 5 })
  await page.mouse.up()
  expect((await page.evaluate(() => window.getSelection()?.toString() ?? '')).trim()).not.toBe('')
})

test('modal scrims use the specified theme alpha without backdrop blur', async ({ page }) => {
  await openPreview(page)
  await page.getByRole('button', { name: 'Settings', exact: true }).click()
  const scrim = page.locator('.gui-scrim-modal')
  await expect(scrim).toBeVisible()
  for (const [theme, expected] of [['light', 'rgba(0, 0, 0, 0.2)'], ['dark', 'rgba(0, 0, 0, 0.4)']] as const) {
    if (theme === 'dark') {
      await page.getByRole('button', { name: 'Close dialog' }).click()
      await page.evaluate(() => localStorage.setItem('vitepress-theme-appearance', 'dark'))
      await page.reload()
      await expect(page.locator('html')).toHaveClass(/(?:^|\s)dark(?:\s|$)/)
      await page.getByRole('button', { name: 'Settings', exact: true }).click()
      await expect(scrim).toBeVisible()
    }
    const style = await scrim.evaluate((element) => ({
      background: getComputedStyle(element).backgroundColor,
      backdropFilter: getComputedStyle(element).backdropFilter,
    }))
    expect(style.background, `${theme} modal scrim`).toBe(expected)
    expect(style.backdropFilter, `${theme} modal backdrop filter`).toBe('none')
  }
})
