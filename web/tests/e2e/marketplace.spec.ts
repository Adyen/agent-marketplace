import { expect, test } from '@playwright/test'

const digest = `sha256:${'a'.repeat(64)}`
const notesDigest = `sha256:${'f'.repeat(64)}`
const configDigest = `sha256:${'1'.repeat(64)}`
const hostileDescription = 'Deploy safely & review <script>alert("xss")</script>'
const mockURL = `http://127.0.0.1:${process.env.E2E_MOCK_PORT || '4311'}`
const appURL = `http://127.0.0.1:${process.env.E2E_APP_PORT || '4321'}`
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
    limit: 25,
  }
}

test.describe('Marketplace browser discovery', () => {
  test('uses the same brand font treatment on homepage and detail pages', async ({ page }) => {
    const getBrandStyle = (selector: string) => page.locator(selector).first().evaluate(element => {
      const style = getComputedStyle(element)
      return { fontFamily: style.fontFamily, fontWeight: style.fontWeight, letterSpacing: style.letterSpacing }
    })

    await page.goto('/')
    const homepageStyle = await getBrandStyle('header a[href="/"] span')

    await page.goto('/s/team-tools/skill/secure-review')
    const detailStyle = await getBrandStyle('header a[href="/"]')

    expect(detailStyle).toEqual(homepageStyle)
  })

  test('searches from the homepage and persists URL-backed filters', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByRole('heading', { name: /A verified index of agent resources/ })).toBeVisible()

    await page.getByLabel('Search', { exact: true }).fill('secure')
    await expect(page).toHaveURL(/q=secure/)
    await page.getByRole('combobox', { name: 'Source', exact: true }).click()
    await page.getByRole('option', { name: 'team-tools', exact: true }).click()

    await expect(page).toHaveURL(/source=team-tools/)
    await expect(page.getByRole('heading', { name: '1', exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: 'team-tools/secure-review', exact: true })).toBeVisible()
  })

  test('filters by kind and omits the all-kinds sentinel from the URL', async ({ page }) => {
    const response = await page.goto('/')
    expect(response?.status()).toBe(200)
    await expect(page.locator('#search-kind')).toHaveText('All resource kinds')

    await page.locator('#search-kind').click()
    await page.getByRole('option', { name: 'Command', exact: true }).click()
    await expect(page).toHaveURL(/kind=command/)
    await expect(page.getByRole('heading', { name: '1', exact: true })).toBeVisible()

    await page.locator('#search-kind').click()
    await page.getByRole('option', { name: 'All resource kinds', exact: true }).click()
    await expect(page).not.toHaveURL(/[?&]kind=/)
    await expect(page.getByRole('heading', { name: '2', exact: true })).toBeVisible()
  })

  test('renders hostile text as text and preserves safe resource links', async ({ page }) => {
    await page.goto('/?q=deploy')
    const card = page.locator('article').filter({ hasText: 'secure-review' })

    await expect(card.getByText(hostileDescription, { exact: true })).toBeVisible()
    await expect(card.locator('script')).toHaveCount(0)
    await expect(card.locator('[onerror]')).toHaveCount(0)
    await expect(card.getByRole('link', { name: 'team-tools/secure-review', exact: true })).toHaveAttribute('href', /\/s\/team-tools\/skill\/secure-review$/)
  })

  test('keeps browser routes Nuxt-owned while the API remains API-only', async ({ page, request }) => {
    const apiLatest = await request.get(`${mockURL}/s/team-tools/skill/secure-review`)
    expect(apiLatest.status()).toBe(404)
    expect(apiLatest.headers()['content-type']).toContain('application/json')

    const latest = await page.goto('/s/team-tools/skill/secure-review')
    expect(latest?.status()).toBe(200)
    expect(latest?.headers()['content-type']).toContain('text/html')
    const exact = await page.goto(`/s/team-tools/skill/secure-review/revisions/${encodeURIComponent(digest)}`)
    expect(exact?.status()).toBe(200)
    expect(exact?.headers()['content-type']).toContain('text/html')
  })

  test('follows a latest resource link and exposes an exact install pin', async ({ page }) => {
    await page.goto('/')
    await page.getByRole('link', { name: 'team-tools/secure-review', exact: true }).click()

    await expect(page).toHaveURL(/\/s\/team-tools\/skill\/secure-review$/)
    await expect(page.getByRole('heading', { name: 'team-tools/secure-review', exact: true })).toBeVisible()
    await expect(page.getByText(digest, { exact: true }).first()).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Revision history', exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: `sha256:${'9'.repeat(64)}`, exact: true })).toBeVisible()
    await expect(page.locator('code')).toHaveText(`agent-marketplace install "${appURL}/s/team-tools/skill/secure-review/revisions/${encodeURIComponent(digest)}"`)
    await expect(page.getByRole('button', { name: /Copy pinned CLI install command/ })).toBeEnabled()
    await expect(page.getByRole('button', { name: /Copy pinned browser URL/ })).toBeEnabled()
    await expect(page.getByText('e2e-analyzer-2', { exact: true })).toBeVisible()
    await expect(page.getByText(`sha256:${'6'.repeat(64)}`, { exact: true })).toBeVisible()
    await expect(page.getByText('remote-reference', { exact: true })).toBeVisible()
  })

  test('follows an exact digest-pinned browser route to safely render hostile primary Markdown', async ({ page }) => {
    const remoteRequests: string[] = []
    page.on('request', request => {
      if (request.url().includes('evil.invalid')) remoteRequests.push(request.url())
    })
    await page.goto(`/s/team-tools/skill/secure-review/revisions/${encodeURIComponent(digest)}`)

    await expect(page).toHaveURL(new RegExp(`/s/team-tools/skill/secure-review/revisions/${encodeURIComponent(digest)}$`))
    await expect(page.getByRole('heading', { name: 'team-tools/secure-review', exact: true })).toBeVisible()
    await expect(page.getByText(digest, { exact: true }).first()).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Revision history', exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: `sha256:${'9'.repeat(64)}`, exact: true })).toHaveAttribute('href', `${appURL}/s/team-tools/skill/secure-review/revisions/sha256%3A${'9'.repeat(64)}`)
    await expect(page.getByRole('heading', { name: 'Inspection preview', exact: true })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Safe review', exact: true })).toBeVisible()
    await expect(page.locator('[data-testid="safe-markdown"] a')).toHaveAttribute('href', 'https://example.com/docs')
    await expect(page.locator('[data-testid="safe-markdown"] a')).toHaveAttribute('target', '_blank')
    await expect(page.locator('[data-testid="safe-markdown"] a')).toHaveAttribute('rel', 'noopener noreferrer')
    const paragraphSpacing = await page.locator('[data-testid="safe-markdown"] p').first().evaluate(element => Number.parseFloat(getComputedStyle(element).marginBottom))
    expect(paragraphSpacing).toBeGreaterThan(0)
    await expect(page.locator('[data-testid="safe-markdown"] img')).toHaveCount(0)
    await expect(page.locator('[data-testid="safe-markdown"] script')).toHaveCount(0)
    await expect(page.locator('[data-testid="safe-markdown"]')).not.toContainText('evil.invalid')
    await expect(page.locator('[data-testid="escaped-raw"]')).toContainText('<script>alert("xss")</script>')
    await expect(page.locator('[data-testid="escaped-raw"] mark')).toHaveAttribute('aria-label', /remote-reference \(warn\)/)
    await expect(page.getByText('Severity: warn', { exact: true })).toBeVisible()
    await expect(page.getByText('Policy: network-policy', { exact: true })).toBeVisible()
    expect(remoteRequests).toEqual([])
  })

  test('lazily selects additional text files by digest without fetching metadata bodies', async ({ page }) => {
    const contentRequests: string[] = []
    page.on('request', request => {
      if (request.url().includes('/inspection/content/')) contentRequests.push(request.url())
    })
    await page.goto(`/s/team-tools/skill/secure-review/revisions/${encodeURIComponent(digest)}`)

    const selector = page.getByLabel('Inspection file')
    await expect(selector).toHaveValue(`sha256:${'e'.repeat(64)}`)
    expect(contentRequests).toEqual([])

    await selector.selectOption(notesDigest)
    await expect(page.getByRole('heading', { name: 'Additional notes', exact: true })).toBeVisible()
    await expect(page.locator('[data-testid="safe-markdown"] script')).toHaveCount(0)
    expect(contentRequests.some(url => url.endsWith(`/inspection/content/${encodeURIComponent(notesDigest)}`))).toBe(true)

    await selector.selectOption(configDigest)
    await expect(page.locator('[data-testid="escaped-selected-raw"]')).toContainText('<script>escaped</script>')
    await expect(page.locator('[data-testid="safe-markdown"]')).toHaveCount(0)
    expect(contentRequests.some(url => url.endsWith(`/inspection/content/${encodeURIComponent(configDigest)}`))).toBe(true)
    expect(contentRequests.some(url => url.includes(`${encodeURIComponent('sha256:2222222222222222222222222222222222222222222222222222222222222222')}`))).toBe(false)
  })

  test('keeps revoked revision metadata visible but disables install command actions', async ({ page }) => {
    const revokedDigest = `sha256:${'b'.repeat(64)}`
    await page.goto(`/s/team-tools/command/deploy-preview/revisions/${encodeURIComponent(revokedDigest)}`)

    await expect(page.getByRole('heading', { name: 'team-tools/deploy-preview', exact: true })).toBeVisible()
    await expect(page.getByRole('alert')).toContainText('This revision is revoked')
    await expect(page.getByText(revokedDigest, { exact: true }).first()).toBeVisible()
    await expect(page.getByRole('button', { name: /Copy pinned CLI install command/ })).toBeDisabled()
    await expect(page.getByRole('button', { name: /Copy pinned browser URL/ })).toBeEnabled()
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
    const response = await page.goto('/')
    const csp = response?.headers()['content-security-policy'] ?? ''
    expect(csp).toContain(`img-src 'self' data: ${mockURL}`)
    await expect(page.getByText(`searches ${mockURL}/api/v1/search`, { exact: true })).toBeVisible()
    await page.getByLabel('Search', { exact: true }).fill('secure')
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
    await expect(relativePage.getByRole('heading', { name: '2', exact: true })).toBeVisible()
    await relativePage.getByLabel('Search', { exact: true }).fill('secure')
    await relativePage.getByRole('button', { name: 'Search' }).click()
    await expect(relativePage.getByRole('heading', { name: '1', exact: true })).toBeVisible()
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
