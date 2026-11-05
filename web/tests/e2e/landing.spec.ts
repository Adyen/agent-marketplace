import { expect, test } from '@playwright/test'

const publicSiteURL = `http://127.0.0.1:${process.env.E2E_PUBLIC_SITE_PORT || '4326'}`

test.describe('Landing hero previews', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto(publicSiteURL)
  })

  // spec: marketplace-web-ui/hero-shows-the-terminal-preview-by-default
  test('shows the existing Terminal preview and caption by default', async ({ page }) => {
    const tabs = page.getByRole('tablist', { name: 'Hero preview' })
    const terminalTab = tabs.getByRole('tab', { name: 'Terminal', exact: true })
    const terminalPanel = page.getByRole('tabpanel', { name: 'Terminal' })

    await expect(terminalTab).toHaveAttribute('aria-selected', 'true')
    await expect(terminalTab).toHaveAttribute('tabindex', '0')
    await expect(terminalPanel.getByLabel(
      'Agent Marketplace terminal UI showing resources grouped by registry and their configured harnesses',
    )).toBeVisible()
    await expect(page.getByText('Select resources, review their targets, then sync.', { exact: true })).toBeVisible()
  })

  // spec: marketplace-web-ui/web-tab-shows-the-catalog-with-a-matching-caption
  test('shows the Web preview and its matching caption when selected', async ({ page }) => {
    const tabs = page.getByRole('tablist', { name: 'Hero preview' })
    await tabs.getByRole('tab', { name: 'Web', exact: true }).click()

    await expect(page.getByRole('tabpanel', { name: 'Web' })).toContainText('Marketplace catalog preview')
    await expect(page.getByText('Browse the catalog, review resources, and copy install commands.', { exact: true })).toBeVisible()
  })

  // spec: marketplace-web-ui/hero-tabs-wrap-keyboard-selection
  test('moves hero tab focus and selection with wrapping keyboard navigation', async ({ page }) => {
    const tabs = page.getByRole('tablist', { name: 'Hero preview' })
    const terminalTab = tabs.getByRole('tab', { name: 'Terminal', exact: true })
    const webTab = tabs.getByRole('tab', { name: 'Web', exact: true })

    await terminalTab.focus()
    await terminalTab.press('ArrowLeft')
    await expect(webTab).toBeFocused()
    await expect(webTab).toHaveAttribute('aria-selected', 'true')

    await webTab.press('ArrowRight')
    await expect(terminalTab).toBeFocused()
    await expect(terminalTab).toHaveAttribute('aria-selected', 'true')

    await terminalTab.press('End')
    await expect(webTab).toBeFocused()
    await expect(webTab).toHaveAttribute('aria-selected', 'true')

    await webTab.press('Home')
    await expect(terminalTab).toBeFocused()
    await expect(terminalTab).toHaveAttribute('aria-selected', 'true')
    await expect(tabs.getByRole('tab', { selected: true })).toHaveCount(1)
    await expect(tabs.locator('[tabindex="0"]')).toHaveCount(1)
  })

  // spec: marketplace-web-ui/hero-and-harness-tabs-remain-independent
  test('keeps hero selection independent of the harness tabs', async ({ page }) => {
    const heroTabs = page.getByRole('tablist', { name: 'Hero preview' })
    const harnessTabs = page.getByRole('tablist', { name: 'Harness target' })
    const webTab = heroTabs.getByRole('tab', { name: 'Web', exact: true })
    const secondHarnessTab = harnessTabs.getByRole('tab').nth(1)

    await webTab.click()
    await secondHarnessTab.click()

    await expect(webTab).toHaveAttribute('id', 'hero-tab-web')
    await expect(secondHarnessTab).toHaveAttribute('id', /^native-output-tab-/)
    await expect(webTab).toHaveAttribute('aria-selected', 'true')
    await expect(secondHarnessTab).toHaveAttribute('aria-selected', 'true')
    await expect(heroTabs.getByRole('tab', { selected: true })).toHaveCount(1)
    await expect(harnessTabs.getByRole('tab', { selected: true })).toHaveCount(1)
    await expect(heroTabs.locator('[tabindex="0"]')).toHaveCount(1)
    await expect(harnessTabs.locator('[tabindex="0"]')).toHaveCount(1)
  })

  // spec: marketplace-web-ui/reduced-motion-switches-hero-tabs-without-transition
  test('switches the preview immediately when reduced motion is enabled', async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.reload()

    const tabs = page.getByRole('tablist', { name: 'Hero preview' })
    await tabs.getByRole('tab', { name: 'Web', exact: true }).click()

    await expect(page.getByRole('tabpanel', { name: 'Web' })).toContainText('Marketplace catalog preview')
    await expect(page.getByText('Browse the catalog, review resources, and copy install commands.', { exact: true })).toBeVisible()
    await expect(tabs.locator('.tab-mark')).toHaveCSS('transition-duration', '0s')
  })
})
