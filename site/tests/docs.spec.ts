import { expect, test } from '@playwright/test'

const base = process.env.SITE_BASE ?? '/'
const release = '3.2.0-rc.13'
const guide = 'how-to/author/add-a-resource-to-a-registry'

for (const version of ['latest', 'preview', release]) {
  const prefix = version === 'latest' ? 'docs/' : `docs/${version}/`

  test(`${version}: deep links, source, sidebar, images and scoped search`, async ({ page }) => {
    const errors: string[] = []
    page.on('pageerror', (error) => errors.push(error.message))
    const response = await page.goto(`${prefix}${guide}`)
    expect(response?.status()).toBe(200)
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('How to add a resource to a registry')
    await expect(page.getByRole('combobox', { name: 'Documentation version' })).toHaveValue(version)
    await expect(page.getByRole('complementary', { name: 'Documentation release' })).toContainText(version === 'preview' ? 'unreleased features' : version === 'latest' ? /\d+\.\d+\.\d+/ : release)
    const sidebar = page.locator('.VPSidebar')
    const links = await sidebar.locator('a[href]').evaluateAll((items) => items.map((item) => item.getAttribute('href')))
    expect(links.length).toBeGreaterThan(20)
    expect(links.every((href) => href?.startsWith(`${base}${prefix}`))).toBe(true)
    await expect(page.getByRole('link', { name: 'Marketplace', exact: true }).first()).toHaveAttribute('href', '/marketplace/')
    const source = page.getByRole('link', { name: 'View page source on GitHub' })
    await expect(source).toHaveAttribute('href', version === 'preview'
      ? `https://github.com/Adyen/agent-marketplace/edit/main/site/docs/${guide}.md`
      : version === 'latest'
        ? new RegExp(`/blob/[a-f0-9]{40}/site/docs/${guide}\\.md$`)
        : `https://github.com/Adyen/agent-marketplace/blob/d03ddd1d33e93911993d5ec0411c831fbe1f87ed/site/docs/${guide}.md`)
    await page.locator('.VPNavBarSearch button').click()
    await page.locator('#localsearch-input').fill('registry')
    await expect(page.locator('.VPLocalSearchBox .result').first()).toBeVisible()
    const results = await page.locator('.VPLocalSearchBox .result').evaluateAll((items) => items.map((item) => item.getAttribute('href')))
    expect(results.length).toBeGreaterThan(0)
    expect(results.every((href) => href?.startsWith(`${base}${prefix}`))).toBe(true)
    if (version === 'latest') expect(results.every((href) => !/\/docs\/(preview|\d+\.\d+\.\d+)/.test(href!))).toBe(true)
    await page.keyboard.press('Escape')
    await sidebar.getByRole('link', { name: 'Quick start', exact: true }).click()
    await expect(page).toHaveURL(new RegExp(`${prefix}quick-start$`))
    // The preview tracks the unreleased quick start, which currently embeds
    // two screenshots; released snapshots keep the original five.
    await expect(page.locator('.vp-doc img')).toHaveCount(version === 'preview' ? 2 : 5)
    await expect.poll(() => page.locator('.vp-doc img').evaluateAll((images) => images.every((image) => (image as HTMLImageElement).complete && (image as HTMLImageElement).naturalWidth > 0))).toBe(true)
    expect(errors).toEqual([])
  })
}

test('selector preserves deep page, updates on history navigation, and works on mobile', async ({ page }) => {
  await page.goto(`docs/${guide}`)
  const selector = page.getByRole('combobox', { name: 'Documentation version' })
  await selector.selectOption(release)
  await expect(page).toHaveURL(new RegExp(`/docs/${release}/${guide}$`))
  await selector.selectOption('preview')
  await expect(page).toHaveURL(new RegExp(`/docs/preview/${guide}$`))
  await page.goBack()
  await expect(selector).toHaveValue(release)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('button', { name: 'mobile navigation' }).click()
  const mobileSelector = page.locator('.VPNavScreen').getByRole('combobox', { name: 'Documentation version' })
  await mobileSelector.selectOption('latest')
  await expect(page).toHaveURL(new RegExp(`/docs/${guide}$`))
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
})

test('landing links remain compatible and unknown releases return 404', async ({ page, request }) => {
  await page.goto('./')
  await expect(page.locator('.VPNavBar').getByRole('link', { name: 'Docs', exact: true })).toHaveAttribute('href', `${base}docs/`)
  await expect(page.getByRole('combobox', { name: 'Documentation version' })).toHaveCount(0)
  const missing = await request.get('docs/0.0.0/not-a-page')
  expect(missing.status()).toBe(404)
})
