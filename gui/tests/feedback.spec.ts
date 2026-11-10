import { expect, test, type Page } from '@playwright/test'
import { startHarness } from './harness'
import { distIndexExists, launchBuiltUserApp } from './app'

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
