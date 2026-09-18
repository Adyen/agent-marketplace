import { expect, test, type Locator, type Page } from '@playwright/test'

async function expectFirstResultWithinViewport(page: Page) {
  const firstResult = page.locator('article').first()
  await expect(firstResult).toBeVisible()
  await expect(firstResult).toBeInViewport({ ratio: 1 })
  expect(await page.evaluate(() => window.scrollY)).toBe(0)
}

test.describe('Marketplace layout at 390px', () => {
  test.use({ viewport: { width: 390, height: 844 } })

  test('keeps the catalog compact and opens filters on demand', async ({ page }) => {
    await page.goto('/')

    await expectFirstResultWithinViewport(page)
    await expect(page.locator('.catalog-sidebar')).toBeHidden()
    await expect(page.getByRole('button', { name: 'Filters', exact: true })).toBeVisible()
    await expect(page.locator('#mobile-resource-filters')).toBeHidden()

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

    await page.getByRole('button', { name: 'Filters', exact: true }).click()
    await expect(page.locator('#mobile-resource-filters')).toBeVisible()
    await expect(page.getByRole('radio', { name: 'All kinds', exact: true })).toBeVisible()
    await expect(page.getByRole('checkbox', { name: 'team-tools', exact: true })).toBeVisible()
  })

  test('keeps long resource identity and command content contained while copy remains visible', async ({ page }) => {
    await page.goto('/r/team-tools/skill/secure-review')
    const heading = page.getByRole('heading', { name: 'secure-review', exact: true })
    const command = page.getByTestId('command-text')
    const copy = page.getByRole('button', { name: /Copy pinned CLI install command/ })

    await heading.evaluate(element => { element.textContent = 'resource-' + 'long'.repeat(50) })
    await command.evaluate(element => { element.textContent += '-' + 'argument'.repeat(80) })

    await expect(copy).toBeVisible()
    const commandScroller = command.locator('..')
    expect(await commandScroller.evaluate(element => element.scrollWidth > element.clientWidth)).toBe(true)
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390)
    const copyBox = await copy.boundingBox()
    expect(copyBox).not.toBeNull()
    expect(copyBox!.x + copyBox!.width).toBeLessThanOrEqual(390)
  })
})

test.describe('Marketplace layout at 768px', () => {
  test.use({ viewport: { width: 768, height: 1024 } })

  test('shows the desktop filter rail and keeps the first result above the fold', async ({ page }) => {
    await page.goto('/')

    await expect(page.getByRole('button', { name: 'Filters', exact: true })).toBeHidden()
    await expect(page.locator('.catalog-sidebar')).toBeVisible()
    await expect(page.getByRole('radio', { name: 'All kinds', exact: true })).toBeVisible()
    await expect(page.getByRole('checkbox', { name: 'team-tools', exact: true })).toBeVisible()
    await expectFirstResultWithinViewport(page)
  })
})

test.describe('Marketplace layout at 1440px', () => {
  test.use({ viewport: { width: 1440, height: 900 } })

  test('shows identity, installation, and the beginning of content above the fold', async ({ page }) => {
    await page.goto('/r/team-tools/skill/secure-review')

    await expect(page.getByRole('heading', { name: 'secure-review', exact: true })).toBeInViewport()
    await expect(page.getByTestId('installation-panel')).toBeInViewport()
    await expect(page.getByRole('button', { name: /Copy pinned CLI install command/ })).toBeInViewport()
    await expect(page.getByRole('tab', { name: 'Content', exact: true })).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByTestId('safe-markdown')).toBeInViewport()
  })

  test('keeps search and results anchored while search applies', async ({ page }) => {
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
    const updateStatus = page.getByTestId('search-update-status')
    const resultsPanel = page.getByTestId('search-results')
    const resultsBody = page.getByTestId('search-results-body')
    const searchBefore = await search.boundingBox()
    const resultBefore = await firstResult.boundingBox()

    expect(searchBefore).not.toBeNull()
    expect(resultBefore).not.toBeNull()
    await expect(updateStatus).toHaveCSS('opacity', '0')
    expect(await page.locator('html').evaluate(element => getComputedStyle(element).scrollbarGutter)).toContain('stable')

    await page.getByLabel('Search', { exact: true }).fill('secure')
    await expect(updateStatus).toHaveCSS('opacity', '1')
    await expect(updateStatus).toContainText('Updating')
    await expect(page).toHaveURL(/q=secure/)
    await expect(firstResult).toBeVisible()
    await expect(page.getByRole('status', { name: 'Loading resources' })).toHaveCount(0)
    await expect(resultsPanel).toHaveAttribute('aria-busy', 'true')
    await expect(resultsBody).toHaveAttribute('inert', '')
    releaseSearch()
    await expect(page.locator('.catalog-results__header p')).toContainText('1 resource')
    await expect(updateStatus).toHaveCSS('opacity', '0')
    await expect(resultsBody).not.toHaveAttribute('inert', '')

    const searchAfter = await search.boundingBox()
    const resultAfter = await firstResult.boundingBox()
    expect(searchAfter).not.toBeNull()
    expect(resultAfter).not.toBeNull()
    expect(Math.abs(searchAfter!.y - searchBefore!.y)).toBeLessThanOrEqual(1)
    expect(Math.abs(searchAfter!.x - searchBefore!.x)).toBeLessThanOrEqual(1)
    expect(Math.abs(resultAfter!.y - resultBefore!.y)).toBeLessThanOrEqual(1)
    expect(Math.abs(resultAfter!.x - resultBefore!.x)).toBeLessThanOrEqual(1)
  })

  test('uses a shared container, sticky filter rail, and unified results column', async ({ page }) => {
    await page.goto('/?kind=skill&registry=team-tools')

    const introduction = page.locator('.marketplace-intro')
    const introductionHeading = page.getByRole('heading', { name: 'e2e-marketplace', exact: true })
    const introductionParagraphs = page.locator('.marketplace-intro__description p')
    const resultsHeader = page.locator('.catalog-results__header')
    const catalogHeading = page.getByRole('heading', { name: 'Resources', exact: true })
    const resultCount = page.locator('.catalog-results__header > p')
    const activeFilters = page.getByLabel('Active filters')
    const search = page.locator('#marketplace-search')
    const sidebar = page.locator('.catalog-sidebar')
    const stickyFilters = page.locator('.catalog-sidebar__sticky')
    const results = page.locator('.catalog-results')
    const [introductionBox, catalogBox, searchBox, sidebarBox, resultsBox] = await Promise.all([
      introductionHeading.boundingBox(),
      catalogHeading.boundingBox(),
      search.boundingBox(),
      sidebar.boundingBox(),
      results.boundingBox(),
    ])

    expect(introductionBox).not.toBeNull()
    expect(catalogBox).not.toBeNull()
    expect(searchBox).not.toBeNull()
    expect(sidebarBox).not.toBeNull()
    expect(resultsBox).not.toBeNull()
    expect(introductionBox!.height).toBeLessThan(50)
    expect((await introduction.boundingBox())!.height).toBeLessThan(140)
    await expect(introductionParagraphs).toHaveCount(2)
    await expect(introductionParagraphs.first()).toHaveCSS('display', 'inline')
    await expect(introductionParagraphs.last()).toHaveCSS('display', 'inline')
    expect(searchBox!.x).toBeGreaterThan(sidebarBox!.x + sidebarBox!.width)
    expect(catalogBox!.y).toBeGreaterThan(searchBox!.y + searchBox!.height)
    expect(Math.abs(introductionBox!.x - sidebarBox!.x)).toBeLessThanOrEqual(1)
    expect(resultsBox!.x).toBeGreaterThan(sidebarBox!.x)
    expect(resultsBox!.width).toBeGreaterThan(600)
    await expect(stickyFilters).toHaveCSS('position', 'sticky')
    await expect(page.getByRole('button', { name: 'Filters', exact: true })).toBeHidden()
    await expect(page.getByRole('radio', { name: 'Skill', exact: true })).toBeChecked()
    await expect(page.getByRole('checkbox', { name: 'team-tools', exact: true })).toBeChecked()
    await expect(resultCount).toContainText('1 resource')
    const skillChip = page.getByRole('button', { name: 'Remove Skill kind filter' })
    const clearFilters = page.getByRole('button', { name: 'Clear', exact: true })
    await expect(skillChip).toBeVisible()
    await expect(page.getByRole('button', { name: 'Remove team-tools registry filter' })).toBeVisible()
    await expect(clearFilters).toBeEnabled()
    await expect(resultsHeader).toHaveCSS('gap', '8px')
    await expect(activeFilters).toHaveCSS('gap', '6px')
    await expect(skillChip).toHaveCSS('min-height', '30px')
    await expect(skillChip).toHaveCSS('gap', '5px')
    await expect(skillChip).toHaveCSS('font-size', '13px')
    await expect(skillChip).toHaveCSS('font-weight', '500')
    const chipPadding = await skillChip.evaluate((element) => {
      const style = getComputedStyle(element)
      return [style.paddingTop, style.paddingRight, style.paddingBottom, style.paddingLeft]
    })
    expect(chipPadding).toEqual(['3px', '8px', '3px', '10px'])
    const clearPadding = await clearFilters.evaluate((element) => {
      const style = getComputedStyle(element)
      return [style.paddingTop, style.paddingRight, style.paddingBottom, style.paddingLeft]
    })
    expect(clearPadding).toEqual(['3px', '6px', '3px', '6px'])

    const firstResult = page.locator('article').first()
    const [headerBox, headingBox, countBox, filtersBox, resultBox, chipBox, clearBox] = await Promise.all([
      resultsHeader.boundingBox(),
      catalogHeading.boundingBox(),
      resultCount.boundingBox(),
      activeFilters.boundingBox(),
      firstResult.boundingBox(),
      skillChip.boundingBox(),
      clearFilters.boundingBox(),
    ])
    expect(countBox!.x - (headingBox!.x + headingBox!.width)).toBeCloseTo(8, 0)
    expect(filtersBox!.y - (headerBox!.y + headerBox!.height)).toBeCloseTo(10, 0)
    expect(resultBox!.y - (filtersBox!.y + filtersBox!.height)).toBeCloseTo(14, 0)
    expect(chipBox!.y + chipBox!.height / 2).toBeCloseTo(clearBox!.y + clearBox!.height / 2, 0)

    const description = firstResult.locator('p').first()
    const copyAction = firstResult.getByRole('button', { name: /Copy install command/ })
    const descriptionBox = await description.boundingBox()
    const copyActionBox = await copyAction.boundingBox()
    expect(descriptionBox).not.toBeNull()
    expect(copyActionBox).not.toBeNull()
    expect(copyActionBox!.x - (descriptionBox!.x + descriptionBox!.width)).toBeGreaterThanOrEqual(16)

    await page.getByRole('button', { name: 'Remove Skill kind filter' }).click()
    await expect(page).not.toHaveURL(/[?&]kind=/)
    await page.getByRole('button', { name: 'Clear', exact: true }).click()
    await expect(page).not.toHaveURL(/[?&]registry=/)
  })

  test('separates empty and error states from active filters', async ({ page }) => {
    await page.route('**/api/v1/search**', async (route) => {
      if (new URL(route.request().url()).searchParams.get('q') === 'error') {
        await route.fulfill({ status: 503, contentType: 'application/json', body: '{}' })
        return
      }
      await route.continue()
    })

    const expectStateGap = async (state: Locator) => {
      await expect(state).toBeVisible()
      const filtersBox = await page.getByLabel('Active filters').boundingBox()
      const stateBox = await state.boundingBox()
      expect(filtersBox).not.toBeNull()
      expect(stateBox).not.toBeNull()
      expect(stateBox!.y - (filtersBox!.y + filtersBox!.height)).toBeGreaterThanOrEqual(12)
    }

    await page.goto('/?q=empty&kind=skill')
    await expectStateGap(page.getByRole('status').filter({ hasText: 'No resources found' }))

    await page.getByLabel('Search', { exact: true }).fill('error')
    await expect(page).toHaveURL(/q=error/)
    await expectStateGap(page.getByRole('alert').filter({ hasText: 'Search could not be completed' }))
  })
})
