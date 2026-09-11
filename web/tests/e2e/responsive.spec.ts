import { expect, test, type Page } from '@playwright/test'

async function expectFirstResultWithinViewport(page: Page) {
  const firstResult = page.locator('article').first()
  await expect(firstResult).toBeVisible()
  await expect(firstResult).toBeInViewport({ ratio: 1 })
  expect(await page.evaluate(() => window.scrollY)).toBe(0)
}

test.describe('Marketplace layout at 390px', () => {
  test.use({ viewport: { width: 390, height: 844 } })

  test('shows the first result above the fold with compact filters and usable actions', async ({ page }) => {
    await page.goto('/')

    await expectFirstResultWithinViewport(page)
    await expect(page.getByRole('button', { name: 'Filters', exact: true })).toBeVisible()
    await expect(page.locator('#search-kind')).toBeHidden()
    await expect(page.locator('#search-registry')).toBeHidden()

    const firstResult = page.locator('article').first()
    const copyAction = firstResult.getByRole('button', { name: 'Copy install command for secure-review' })
    await expect(copyAction).toBeVisible()
    await expect(copyAction).toHaveText('Copy install command')
    const description = firstResult.getByText('Deploy safely & review <script>alert("xss")</script>', { exact: true })
    const actionBox = await copyAction.boundingBox()
    const descriptionBox = await description.boundingBox()
    expect(actionBox).not.toBeNull()
    expect(descriptionBox).not.toBeNull()
    expect(actionBox!.height).toBeGreaterThanOrEqual(32)
    expect(actionBox!.y).toBeGreaterThan(descriptionBox!.y + descriptionBox!.height)
    await expect(description).toHaveCSS('white-space', 'nowrap')
    await expect(firstResult.getByText('View details', { exact: true })).toHaveCount(0)

    const unavailableResult = page.locator('article').last()
    const firstResultBox = await firstResult.boundingBox()
    const unavailableResultBox = await unavailableResult.boundingBox()
    expect(firstResultBox).not.toBeNull()
    expect(unavailableResultBox).not.toBeNull()
    expect(Math.abs(firstResultBox!.height - unavailableResultBox!.height)).toBeLessThanOrEqual(1)

    await page.getByRole('button', { name: 'Filters', exact: true }).click()
    await expect(page.locator('#search-kind')).toBeVisible()
    await expect(page.locator('#search-registry')).toBeVisible()
  })
})

test.describe('Marketplace layout at 768px', () => {
  test.use({ viewport: { width: 768, height: 1024 } })

  test('keeps filters inline and the first result above the fold', async ({ page }) => {
    await page.goto('/')

    await expect(page.getByRole('button', { name: 'Filters', exact: true })).toBeHidden()
    await expect(page.locator('#search-kind')).toBeVisible()
    await expect(page.locator('#search-registry')).toBeVisible()
    await expectFirstResultWithinViewport(page)
  })
})

test.describe('Marketplace layout at 1440px', () => {
  test.use({ viewport: { width: 1440, height: 900 } })

  test('keeps search controls and the result list anchored while search applies', async ({ page }) => {
    let releaseSearch = () => {}
    const delayedSearch = new Promise<void>((resolve) => {
      releaseSearch = resolve
    })
    await page.route('**/api/v1/search**', async (route) => {
      if (new URL(route.request().url()).searchParams.get('q') === 'secure') await delayedSearch
      await route.continue()
    })
    await page.goto('/')
    await page.locator('article').last().evaluate((element) => {
      element.style.minHeight = '1000px'
    })
    expect(await page.evaluate(() => document.documentElement.scrollHeight > window.innerHeight)).toBe(true)

    const search = page.locator('#marketplace-search')
    const firstResult = page.locator('article').first()
    const clearFilters = page.getByRole('button', { name: 'Clear filters', exact: true })
    const updateStatus = page.getByTestId('search-update-status')
    const resultsPanel = page.getByTestId('search-results')
    const resultsBody = page.getByTestId('search-results-body')
    const searchBefore = await search.boundingBox()
    const resultBefore = await firstResult.boundingBox()

    expect(searchBefore).not.toBeNull()
    expect(resultBefore).not.toBeNull()
    await expect(clearFilters).toHaveCount(0)
    await expect(updateStatus).toHaveCSS('opacity', '0')
    expect(await page.locator('html').evaluate(element => getComputedStyle(element).scrollbarGutter)).toContain('stable')

    await page.getByLabel('Search', { exact: true }).fill('secure')
    await expect(clearFilters).toBeVisible()
    await expect(clearFilters).toBeEnabled()
    await expect(clearFilters).toHaveCSS('cursor', 'pointer')
    await expect(updateStatus).toHaveCSS('opacity', '1')
    await expect(updateStatus).toHaveText('Updating')
    await expect(page).toHaveURL(/q=secure/)
    await expect(firstResult).toBeVisible()
    await expect(page.getByRole('status', { name: 'Loading resources' })).toHaveCount(0)
    await expect(resultsPanel).toHaveAttribute('aria-busy', 'true')
    await expect(resultsBody).toHaveAttribute('inert', '')
    releaseSearch()
    await expect(page.getByText('· 1 total', { exact: true })).toBeVisible()
    await expect(updateStatus).toHaveCSS('opacity', '0')
    await expect(resultsBody).not.toHaveAttribute('inert', '')
    await expect(page.getByText('matching your search', { exact: true })).toHaveCount(0)
    expect(await page.evaluate(() => document.documentElement.scrollHeight <= window.innerHeight)).toBe(true)

    const searchAfter = await search.boundingBox()
    const resultAfter = await firstResult.boundingBox()
    expect(searchAfter).not.toBeNull()
    expect(resultAfter).not.toBeNull()
    expect(Math.abs(searchAfter!.y - searchBefore!.y)).toBeLessThanOrEqual(1)
    expect(Math.abs(searchAfter!.x - searchBefore!.x)).toBeLessThanOrEqual(1)
    expect(Math.abs(resultAfter!.y - resultBefore!.y)).toBeLessThanOrEqual(1)
    expect(Math.abs(resultAfter!.x - resultBefore!.x)).toBeLessThanOrEqual(1)
  })

  test('keeps the introduction above the full-width searchable catalog', async ({ page }) => {
    await page.goto('/?kind=skill&registry=team-tools')

    const search = await page.locator('#marketplace-search').boundingBox()
    expect(search).not.toBeNull()
    expect(search!.width).toBeGreaterThan(1000)

    const introductionHeading = await page.getByRole('heading', { name: 'e2e-marketplace', exact: true }).boundingBox()
    const catalogHeading = await page.getByRole('heading', { name: 'Resources', exact: true }).boundingBox()
    const resultCount = await page.getByText('· 1 total', { exact: true }).boundingBox()
    expect(introductionHeading).not.toBeNull()
    expect(catalogHeading).not.toBeNull()
    expect(resultCount).not.toBeNull()
    expect(catalogHeading!.y).toBeGreaterThan(introductionHeading!.y + introductionHeading!.height)
    expect(resultCount!.y).toBeLessThan(catalogHeading!.y + catalogHeading!.height)
    expect(catalogHeading!.y).toBeLessThan(resultCount!.y + resultCount!.height)
    expect(search!.y).toBeGreaterThan(catalogHeading!.y + catalogHeading!.height)

    await expect(page.getByRole('button', { name: 'Filters', exact: true })).toBeHidden()
    await expect(page.locator('#search-kind')).toBeVisible()
    await expect(page.locator('#search-registry')).toBeVisible()
    const clearFilters = page.getByRole('button', { name: 'Clear filters', exact: true })
    const searchInput = page.locator('#search-query')
    const kindFilter = page.locator('#search-kind')
    const registryFilter = page.locator('#search-registry')
    await expect(clearFilters).toBeEnabled()
    const [searchInputBox, kindFilterBox, registryFilterBox, clearFiltersBox] = await Promise.all([
      searchInput.boundingBox(),
      kindFilter.boundingBox(),
      registryFilter.boundingBox(),
      clearFilters.boundingBox(),
    ])
    expect(searchInputBox).not.toBeNull()
    expect(kindFilterBox).not.toBeNull()
    expect(registryFilterBox).not.toBeNull()
    expect(clearFiltersBox).not.toBeNull()
    expect(Math.abs(clearFiltersBox!.y - kindFilterBox!.y)).toBeLessThanOrEqual(1)
    expect([
      kindFilterBox!.height,
      registryFilterBox!.height,
      clearFiltersBox!.height,
    ]).toEqual([
      searchInputBox!.height,
      searchInputBox!.height,
      searchInputBox!.height,
    ])
    const searchFontSize = await searchInput.evaluate(element => getComputedStyle(element).fontSize)
    await expect(kindFilter).toHaveCSS('font-size', searchFontSize)
    await expect(registryFilter).toHaveCSS('font-size', searchFontSize)
    await expect(clearFilters).toHaveCSS('font-size', searchFontSize)
    const kindValueBox = await kindFilter.locator('[data-slot="value"]').boundingBox()
    const registryValueBox = await registryFilter.getByTestId('registry-filter-first').boundingBox()
    expect(kindValueBox).not.toBeNull()
    expect(registryValueBox).not.toBeNull()
    const kindInset = kindValueBox!.x - kindFilterBox!.x
    const registryInset = registryValueBox!.x - registryFilterBox!.x
    expect(Math.abs(kindInset - registryInset)).toBeLessThanOrEqual(1)
    const searchX = search!.x
    await kindFilter.click()
    await expect(page.getByRole('option', { name: 'Skill', exact: true })).toBeVisible()
    expect(await page.locator('body').evaluate(element => element.style.overflow)).not.toBe('hidden')
    expect(Math.abs((await page.locator('#marketplace-search').boundingBox())!.x - searchX)).toBeLessThanOrEqual(1)
    await page.keyboard.press('Escape')

    const firstResult = page.locator('article').first()
    const description = firstResult.locator('p').first()
    const copyAction = firstResult.getByRole('button', { name: /Copy install command/ })
    const descriptionBox = await description.boundingBox()
    const copyActionBox = await copyAction.boundingBox()
    expect(descriptionBox).not.toBeNull()
    expect(copyActionBox).not.toBeNull()
    expect(copyActionBox!.x - (descriptionBox!.x + descriptionBox!.width)).toBeGreaterThanOrEqual(16)

    const availableResultBox = await firstResult.boundingBox()
    const unavailableResultBox = await page.locator('article').last().boundingBox()
    expect(availableResultBox).not.toBeNull()
    expect(unavailableResultBox).not.toBeNull()
    expect(Math.abs(availableResultBox!.height - unavailableResultBox!.height)).toBeLessThanOrEqual(1)

    await clearFilters.click()
    await expect(page).not.toHaveURL(/[?&]registry=/)
    await expect(page).not.toHaveURL(/[?&]kind=/)
  })
})
