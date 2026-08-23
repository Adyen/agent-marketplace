import { expect, test } from '@playwright/test'

const digest = `sha256:${'a'.repeat(64)}`
const hostileDescription = 'Deploy safely & review <script>alert("xss")</script>'
const mockURL = `http://127.0.0.1:${process.env.E2E_MOCK_PORT || '4311'}`
const relativeAppURL = `http://127.0.0.1:${process.env.E2E_RELATIVE_APP_PORT || '4322'}`

function searchResult() {
  return {
    items: [{
      source_id: 'team-tools',
      source_slug: 'team-tools',
      kind: 'skill',
      name: 'secure-review',
      revision: digest,
      content_digest: digest,
      description: hostileDescription,
    }],
    total: 1,
    offset: 0,
    limit: 12,
  }
}

test.describe('Marketplace browser discovery', () => {
  test('searches from the homepage and persists URL-backed filters', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByRole('heading', { name: /Discover tools for your next agent workflow/ })).toBeVisible()

    await page.getByLabel('Search resources').fill('secure')
    await page.getByRole('textbox', { name: 'Source', exact: true }).fill('team-tools')
    await page.getByRole('button', { name: 'Search' }).click()

    await expect(page).toHaveURL(/q=secure/)
    await expect(page).toHaveURL(/source=team-tools/)
    await expect(page.getByRole('heading', { name: '1 resources' })).toBeVisible()
    await expect(page.getByRole('link', { name: 'secure-review', exact: true })).toBeVisible()
  })

  test('filters by kind and omits the all-kinds sentinel from the URL', async ({ page }) => {
    const response = await page.goto('/')
    expect(response?.status()).toBe(200)
    await expect(page.locator('#search-kind')).toHaveText('All resource kinds')

    await page.locator('#search-kind').click()
    await page.getByRole('option', { name: 'Command', exact: true }).click()
    await page.getByRole('button', { name: 'Search' }).click()
    await expect(page).toHaveURL(/kind=command/)
    await expect(page.getByRole('heading', { name: '1 resources' })).toBeVisible()

    await page.locator('#search-kind').click()
    await page.getByRole('option', { name: 'All resource kinds', exact: true }).click()
    await page.getByRole('button', { name: 'Search' }).click()
    await expect(page).not.toHaveURL(/[?&]kind=/)
    await expect(page.getByRole('heading', { name: '2 resources' })).toBeVisible()
  })

  test('renders hostile text as text and preserves safe resource links', async ({ page }) => {
    await page.goto('/?q=deploy')
    const card = page.locator('article').filter({ hasText: 'secure-review' })

    await expect(card.getByText(hostileDescription, { exact: true })).toBeVisible()
    await expect(card.locator('script')).toHaveCount(0)
    await expect(card.locator('[onerror]')).toHaveCount(0)
    await expect(card.getByRole('link', { name: 'secure-review', exact: true })).toHaveAttribute('href', new RegExp(`/revisions/${encodeURIComponent(digest)}$`))
  })

  test('follows a digest-pinned resource link to verified detail', async ({ page }) => {
    await page.goto('/')
    await page.getByRole('link', { name: 'secure-review', exact: true }).click()

    await expect(page).toHaveURL(new RegExp(`/s/team-tools/skill/secure-review/revisions/${encodeURIComponent(digest)}$`))
    await expect(page.getByRole('heading', { name: 'secure-review', exact: true })).toBeVisible()
    await expect(page.getByText(digest, { exact: true }).first()).toBeVisible()
    await expect(page.getByText('No findings were reported for this revision.', { exact: true })).toBeVisible()
  })

  test('shows the source history empty state', async ({ page }) => {
    await page.goto('/sources/team-tools/history')

    await expect(page.getByRole('heading', { name: 'team-tools', exact: true })).toBeVisible()
    await expect(page.getByText('No source occurrences have been recorded.', { exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Source overview', exact: true })).toHaveAttribute('href', '/sources/team-tools')
  })

  test('supports skip-link and form keyboard navigation', async ({ page }) => {
    await page.goto('/')
    await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur())
    await page.keyboard.press('Tab')
    await expect(page.locator('a[href="#main-content"]')).toBeFocused()
    await page.keyboard.press('Enter')
    await expect(page.locator('#main-content')).toBeFocused()

    await page.locator('#search-query').focus()
    await page.keyboard.press('Tab')
    await expect(page.locator('#search-kind')).toBeFocused()
  })

  test('honors reduced-motion media preferences', async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/')

    const mediaState = await page.evaluate(() => ({
      reducedMotion: window.matchMedia('(prefers-reduced-motion: reduce)').matches,
      scrollBehavior: getComputedStyle(document.documentElement).scrollBehavior,
    }))
    expect(mediaState).toEqual({ reducedMotion: true, scrollBehavior: 'auto' })
  })

  test('uses absolute and same-origin API bases', async ({ page, context }) => {
    const absoluteRequests: string[] = []
    page.on('request', request => {
      if (request.url().includes('/api/v1/')) absoluteRequests.push(request.url())
    })
    await page.goto('/')
    await expect(page.getByText(`${mockURL}/api/v1/search`, { exact: true })).toBeVisible()
    await page.getByLabel('Search resources').fill('secure')
    await page.getByRole('button', { name: 'Search' }).click()
    await expect(page).toHaveURL(/q=secure/)
    expect(absoluteRequests.some(url => url.startsWith(`${mockURL}/api/v1/`))).toBe(true)

    const relativePage = await context.newPage()
    const sameOriginRequests: string[] = []
    relativePage.on('request', request => {
      if (request.url().includes('/api/v1/search')) sameOriginRequests.push(request.url())
    })
    await relativePage.route('**/api/v1/search**', route => route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(searchResult()),
    }))
    await relativePage.goto(relativeAppURL)
    await expect(relativePage.getByRole('heading', { name: '2 resources' })).toBeVisible()
    await relativePage.getByLabel('Search resources').fill('secure')
    await relativePage.getByRole('button', { name: 'Search' }).click()
    await expect(relativePage.getByRole('heading', { name: '1 resources' })).toBeVisible()
    expect(sameOriginRequests.some(url => {
      const requestURL = new URL(url)
      return requestURL.origin === relativeAppURL && requestURL.pathname === '/api/v1/search'
    })).toBe(true)
    await relativePage.close()
  })

  test('keeps production homepage navigation under the basic performance budget', async ({ page }) => {
    await page.goto('/')
    const domContentLoaded = await page.evaluate(() => performance.getEntriesByType('navigation')[0]?.domContentLoadedEventEnd || 0)
    expect(domContentLoaded).toBeGreaterThan(0)
    expect(domContentLoaded).toBeLessThan(3000)
  })
})
