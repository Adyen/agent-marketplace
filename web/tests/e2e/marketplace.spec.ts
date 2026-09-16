import { expect, test } from '@playwright/test'

const digest = `sha256:${'a'.repeat(64)}`
const notesDigest = `sha256:${'f'.repeat(64)}`
const configDigest = `sha256:${'1'.repeat(64)}`
const hostileDescription = 'Deploy safely & review <script>alert("xss")</script>'
const mockURL = `http://127.0.0.1:${process.env.E2E_MOCK_PORT || '4311'}`
const appURL = `http://127.0.0.1:${process.env.E2E_APP_PORT || '4321'}`
const relativeAppURL = `http://127.0.0.1:${process.env.E2E_RELATIVE_APP_PORT || '4322'}`
const relativeAppBaseURL = `${relativeAppURL}/marketplace/`
const sharedOriginURL = `http://127.0.0.1:${process.env.E2E_SHARED_ORIGIN_PORT || '4323'}`
const sharedAppearanceKey = 'vitepress-theme-appearance'

function searchResult() {
  return {
    items: [{
      registry_id: 'team-tools',
      registry_slug: 'team-tools',
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
    const homepageStyle = await getBrandStyle('.am-site-nav__brand')

    await page.goto('/r/team-tools/skill/secure-review')
    const detailStyle = await getBrandStyle('.am-site-nav__brand')

    expect(detailStyle).toEqual(homepageStyle)
  })

  test('shares navigation and appearance across both frontends', async ({ page }) => {
    const fontStatus = () => page.evaluate(() => {
      const bodyStyle = getComputedStyle(document.body)
      return {
        bodyFamily: bodyStyle.fontFamily,
        fontSmoothing: bodyStyle.getPropertyValue('-webkit-font-smoothing'),
        textRendering: bodyStyle.textRendering,
        downloadedFonts: performance.getEntriesByType('resource')
          .filter(entry => /\.(?:woff2?|ttf|otf)(?:\?|$)/.test(entry.name))
          .length,
      }
    })

    await page.goto(sharedOriginURL)
    await page.evaluate(key => localStorage.setItem(key, 'light'), sharedAppearanceKey)
    await page.reload()

    await expect(page.locator('.VPNav')).toHaveCount(1)
    const landingNav = page.getByRole('navigation', { name: 'Main Navigation' })
    await expect(page.locator('.VPNavBarTitle').getByRole('link', { name: 'agent-marketplace', exact: true })).toHaveAttribute('href', '/')
    await expect(landingNav.getByRole('link', { name: 'Marketplace', exact: true })).toHaveAttribute('target', '_self')
    await expect(landingNav.getByRole('link', { name: 'Marketplace', exact: true })).toHaveAttribute('href', '../marketplace/')
    await expect(landingNav.getByRole('link', { name: 'Docs', exact: true })).toHaveAttribute('href', '/#install')
    await expect(page.locator('link[rel="prefetch"][href="/marketplace/"]')).toHaveCount(1)
    await expect(page.locator('html')).not.toHaveClass(/\bdark\b/)
    await expect.poll(fontStatus).toEqual({
      bodyFamily: 'system-ui, sans-serif',
      fontSmoothing: 'antialiased',
      textRendering: 'optimizelegibility',
      downloadedFonts: 0,
    })

    await page.goto(`${sharedOriginURL}/README`)
    await expect(page.locator('.VPNav')).toHaveCount(1)
    expect(await page.locator('.VPNavBarTitle').getByRole('link', { name: 'agent-marketplace', exact: true }).getAttribute('aria-current')).toBeNull()
    await page.goto(sharedOriginURL)

    const landingSwitch = page.locator('.VPNavBarAppearance .VPSwitchAppearance')
    await expect(landingSwitch).toHaveAttribute('title', 'Switch to dark theme')
    await expect(landingSwitch).toHaveAttribute('aria-checked', 'false')
    await landingSwitch.click()
    await expect(page.locator('html')).toHaveClass(/\bdark\b/)
    await expect.poll(() => page.evaluate(key => localStorage.getItem(key), sharedAppearanceKey)).toBe('dark')

    await landingNav.getByRole('link', { name: 'Marketplace', exact: true }).click()
    await expect(page).toHaveURL(`${sharedOriginURL}/marketplace/`)

    const marketplaceNav = page.getByRole('navigation', { name: 'Product navigation' })
    await expect(marketplaceNav.getByRole('link', { name: 'agent-marketplace', exact: true })).toHaveAttribute('href', '/')
    await expect(marketplaceNav.getByRole('link', { name: 'Marketplace', exact: true })).toHaveAttribute('href', '/marketplace/')
    await expect(marketplaceNav.getByRole('link', { name: 'Marketplace', exact: true })).toHaveAttribute('aria-current', 'page')
    await expect(marketplaceNav.getByRole('link', { name: 'Docs', exact: true })).toHaveAttribute('href', '/#install')
    await expect(page.locator('html')).toHaveClass(/\bdark\b/)
    await expect(page.locator('link[href*="nuxt-fonts-global"]')).toHaveCount(0)
    await expect.poll(fontStatus).toEqual({
      bodyFamily: 'system-ui, sans-serif',
      fontSmoothing: 'antialiased',
      textRendering: 'optimizelegibility',
      downloadedFonts: 0,
    })

    const marketplaceSwitch = marketplaceNav.getByRole('switch', { name: 'Switch to light theme' })
    await expect(marketplaceSwitch).toHaveAttribute('aria-checked', 'true')
    await expect(marketplaceSwitch).toHaveCSS('cursor', 'pointer')
    await marketplaceSwitch.click()
    await expect(page.locator('html')).not.toHaveClass(/\bdark\b/)
    await expect.poll(() => page.evaluate(key => localStorage.getItem(key), sharedAppearanceKey)).toBe('light')

    await marketplaceNav.getByRole('link', { name: 'agent-marketplace', exact: true }).click()
    await expect(page).toHaveURL(`${sharedOriginURL}/`)
    await expect(page.locator('html')).not.toHaveClass(/\bdark\b/)
    await expect(page.evaluate(key => localStorage.getItem(key), sharedAppearanceKey)).resolves.toBe('light')
  })

  test('keeps Nuxt color scales ordered in both themes', async ({ page }) => {
    const scales = ['ink', 'focus', 'ready', 'withheld', 'revoked']

    await page.goto('/')
    for (const preference of ['light', 'dark']) {
      await page.evaluate(
        ({ key, value }) => localStorage.setItem(key, value),
        { key: sharedAppearanceKey, value: preference },
      )
      await page.reload()

      const luminances = await page.evaluate((names) => {
        const probe = document.createElement('span')
        document.body.appendChild(probe)

        const result = Object.fromEntries(names.map((name) => {
          const shades = [50, 500, 950].map((shade) => {
            probe.style.color = `var(--color-${name}-${shade})`
            const rendered = getComputedStyle(probe).color
            const channels = (rendered.match(/[0-9]*\.?[0-9]+/g) ?? []).slice(0, 3).map(Number)
            const divisor = channels.some(channel => channel > 1) ? 255 : 1
            return channels.reduce((sum, channel) => sum + channel / divisor, 0) / 3
          })
          return [name, shades]
        }))

        probe.remove()
        return result
      }, scales)

      for (const scale of scales) {
        const [light, middle, dark] = luminances[scale] as number[]
        expect(light, `${scale}-50 should be lighter in ${preference} mode`).toBeGreaterThan(middle)
        expect(middle, `${scale}-500 should be lighter than ${scale}-950 in ${preference} mode`).toBeGreaterThan(dark)
      }
    }
  })

  test('keeps dense Marketplace copy readable in dark mode', async ({ page }) => {
    await page.goto('/')
    await page.evaluate(key => localStorage.setItem(key, 'dark'), sharedAppearanceKey)
    await page.reload()
    await expect(page.locator('html')).toHaveClass(/\bdark\b/)

    const typography = await page.evaluate(() => {
      const metrics = (selector: string) => {
        const element = document.querySelector(selector)
        if (!element) throw new Error(`Missing typography probe: ${selector}`)
        const style = getComputedStyle(element)
        return {
          size: Number.parseFloat(style.fontSize),
          weight: Number.parseInt(style.fontWeight, 10),
        }
      }

      return {
        kind: metrics('article:first-of-type span.font-mono'),
        coordinate: metrics('article:first-of-type h2'),
        description: metrics('article:first-of-type p.text-toned'),
        copyAction: metrics('article:first-of-type button'),
      }
    })

    expect(typography.kind.size).toBeGreaterThanOrEqual(13)
    expect(typography.coordinate.size).toBeGreaterThanOrEqual(15)
    expect(typography.description.size).toBeGreaterThanOrEqual(15)
    expect(typography.copyAction.size).toBeGreaterThanOrEqual(13)
    expect(typography.kind.weight).toBeGreaterThanOrEqual(450)
    expect(typography.coordinate.weight).toBeGreaterThanOrEqual(450)
    expect(typography.description.weight).toBeGreaterThanOrEqual(450)
  })

  test('exposes the shared navigation from a keyboard-operable mobile menu', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })

    for (const path of ['/', '/marketplace/']) {
      await page.goto(`${sharedOriginURL}${path}`)
      const isLanding = path === '/'
      const nav = page.getByRole('navigation', { name: isLanding ? 'Main Navigation' : 'Product navigation' })
      const menuButton = page.getByRole('button', { name: isLanding ? 'mobile navigation' : 'Toggle navigation' })
      const marketplaceLink = isLanding
        ? page.locator('#VPNavScreen').getByRole('link', { name: 'Marketplace', exact: true })
        : nav.getByRole('link', { name: 'Marketplace', exact: true })

      const viewport = await page.evaluate(() => ({
        width: document.documentElement.clientWidth,
        scrollWidth: document.documentElement.scrollWidth,
      }))
      expect(viewport.scrollWidth).toBeLessThanOrEqual(viewport.width)

      await expect(menuButton).toBeVisible()
      await expect(menuButton).toHaveAttribute('aria-expanded', 'false')
      await expect(marketplaceLink).not.toBeVisible()

      await menuButton.focus()
      await menuButton.press('Enter')
      await expect(menuButton).toHaveAttribute('aria-expanded', 'true')
      await expect(marketplaceLink).toBeVisible()
      if (isLanding) {
        await menuButton.press('Enter')
      }
      else {
        await menuButton.press('Escape')
        await expect(menuButton).toBeFocused()
      }
      await expect(menuButton).toHaveAttribute('aria-expanded', 'false')
      await expect(marketplaceLink).not.toBeVisible()
    }
  })

  test('explains direct resource installation without requiring Marketplace registration', async ({ page, context }) => {
    await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: appURL })
    await page.goto('/')

    await expect(page.getByRole('heading', { name: 'e2e-marketplace', exact: true })).toBeVisible()
    await expect(page.getByText('Browse resources for AI agents, with every revision pinned and analyzed before publication.', { exact: true })).toBeVisible()
    await expect(page.getByText(/To use a resource, install the.*agent-marketplace.*CLI, then copy and run its install command/)).toBeVisible()
    await expect(page.getByText(`agent-marketplace install "${appURL}"`, { exact: true })).toHaveCount(0)
    const copyInstall = page.getByRole('button', { name: 'Copy install command for secure-review', exact: true })
    await copyInstall.click()
    await expect(page.getByRole('button', { name: 'Install command copied for secure-review', exact: true })).toHaveText('Copied')
    await expect(page.getByRole('heading', { name: 'Resources', exact: true })).toBeVisible()
    await expect(page.getByText('01 / connect', { exact: true })).toHaveCount(0)
    await expect(page.getByText('02 / discover', { exact: true })).toHaveCount(0)
    await expect(page.getByText('agent-marketplace search', { exact: true })).toHaveCount(0)
    await expect(page.getByText(/read-only/i)).toHaveCount(0)
    const lastResource = page.locator('article').last()
    const unavailableStatus = lastResource.getByRole('button', { name: 'Not available for deploy-preview. More information' })
    await expect(unavailableStatus).toHaveText('Not available')
    const unavailableRestBackground = await unavailableStatus.evaluate(el => getComputedStyle(el).backgroundColor)
    await unavailableStatus.hover()
    await expect(unavailableStatus).toHaveCSS('background-color', unavailableRestBackground)
    await expect(page.locator('[data-slot="content"]')).toContainText('Installation is disabled because this revision is revoked.')
    await expect(lastResource.getByText('review', { exact: true })).toHaveCount(0)
    await expect(page.getByText(/on this page cannot be installed/)).toHaveCount(0)
  })

  test('searches from the homepage and persists URL-backed filters', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByRole('heading', { name: 'Resources', exact: true })).toBeVisible()

    await page.getByLabel('Search', { exact: true }).fill('secure')
    await expect(page).toHaveURL(/q=secure/)
    await page.getByRole('combobox', { name: 'Registry', exact: true }).click()
    const longRegistry = page.getByRole('option', { name: 'registry-with-an-extremely-long-name', exact: true })
    await expect(longRegistry).toBeVisible()
    await expect(longRegistry.locator('[data-slot="itemLabel"]')).toHaveCSS('text-overflow', 'ellipsis')
    await page.getByRole('option', { name: 'team-tools', exact: true }).click()
    await page.getByRole('option', { name: 'community-tools', exact: true }).click()
    await page.keyboard.press('Escape')

    await expect(page).toHaveURL(/registry=team-tools&registry=community-tools/)
    await expect(page.locator('#search-registry')).toContainText('team-tools')
    await expect(page.locator('#search-registry')).toContainText('+1 more')
    await expect(page.getByText('· 1 total', { exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: 'team-tools/secure-review', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Search', exact: true })).toHaveCount(0)

    await page.getByRole('button', { name: 'Clear filters', exact: true }).click()
    await expect(page.getByLabel('Search', { exact: true })).toHaveValue('')
    await expect(page).not.toHaveURL(/[?&]q=/)
  })

  test('filters by kind and uses the empty placeholder for all kinds', async ({ page }) => {
    const response = await page.goto('/')
    expect(response?.status()).toBe(200)
    await expect(page.locator('#search-kind')).toHaveText('All resource kinds')

    await page.locator('#search-kind').click()
    await expect(page.getByRole('option', { name: 'All resource kinds', exact: true })).toHaveCount(0)
    await page.getByRole('option', { name: 'Command', exact: true }).click()
    await expect(page).toHaveURL(/kind=command/)
    await expect(page.getByText('· 1 total', { exact: true })).toBeVisible()

    await page.getByRole('button', { name: 'Clear filters', exact: true }).click()
    await expect(page).not.toHaveURL(/[?&]kind=/)
    await expect(page.getByText('· 2 total', { exact: true })).toBeVisible()
  })

  test('renders hostile text as text and preserves safe resource links', async ({ page }) => {
    await page.goto('/?q=deploy')
    const card = page.locator('article').filter({ hasText: 'secure-review' })

    await expect(card.getByText(hostileDescription, { exact: true })).toBeVisible()
    await expect(card.locator('script')).toHaveCount(0)
    await expect(card.locator('[onerror]')).toHaveCount(0)
    await expect(card.getByRole('link', { name: 'team-tools/secure-review', exact: true })).toHaveAttribute('href', `/r/team-tools/skill/secure-review/revisions/${encodeURIComponent(digest)}`)
  })

  test('keeps browser routes Nuxt-owned while the API remains API-only', async ({ page, request }) => {
    const apiLatest = await request.get(`${mockURL}/r/team-tools/skill/secure-review`)
    expect(apiLatest.status()).toBe(404)
    expect(apiLatest.headers()['content-type']).toContain('application/json')

    const latest = await page.goto('/r/team-tools/skill/secure-review')
    expect(latest?.status()).toBe(200)
    expect(latest?.headers()['content-type']).toContain('text/html')
    const exact = await page.goto(`/r/team-tools/skill/secure-review/revisions/${encodeURIComponent(digest)}`)
    expect(exact?.status()).toBe(200)
    expect(exact?.headers()['content-type']).toContain('text/html')
  })

  test('follows an exact resource link and exposes the same install pin', async ({ page }) => {
    const previousDigest = `sha256:${'9'.repeat(64)}`
    await page.goto('/')
    await page.getByRole('link', { name: 'team-tools/secure-review', exact: true }).click()

    await expect(page).toHaveURL(`${appURL}/r/team-tools/skill/secure-review/revisions/${encodeURIComponent(digest)}`)
    await expect(page.getByRole('heading', { name: 'team-tools/secure-review', exact: true })).toBeVisible()
    await expect(page.getByText(digest, { exact: true }).first()).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Revision history', exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: previousDigest, exact: true })).toBeVisible()
    await expect(page.locator('code')).toHaveText(`agent-marketplace install "${appURL}/r/team-tools/skill/secure-review/revisions/${encodeURIComponent(digest)}"`)
    await expect(page.getByRole('button', { name: /Copy pinned CLI install command/ })).toBeEnabled()
    await expect(page.getByRole('button', { name: /Copy detail URL/ })).toBeEnabled()
    await expect(page.getByRole('heading', { name: 'Security findings', exact: true })).toBeVisible()
    await expect(page.getByText('e2e-analyzer-2', { exact: true })).toBeVisible()
    await expect(page.getByText(`sha256:${'6'.repeat(64)}`, { exact: true })).toBeVisible()
    await expect(page.getByText('remote-reference', { exact: true })).toBeVisible()

    const exactRevisionResponse = page.waitForResponse(response => response.url().includes(`/revisions/${encodeURIComponent(previousDigest)}`) && !response.url().includes('/inspection'))
    await page.getByRole('link', { name: previousDigest, exact: true }).click()
    expect((await exactRevisionResponse).status()).toBe(200)
    await expect(page).toHaveURL(`${appURL}/r/team-tools/skill/secure-review/revisions/${encodeURIComponent(previousDigest)}`)
    await expect(page.getByRole('alert', { name: 'This revision is revoked' })).toContainText('superseded policy')
  })

  test('follows an exact digest-pinned browser route to safely render hostile primary Markdown', async ({ page }) => {
    const remoteRequests: string[] = []
    page.on('request', request => {
      if (request.url().includes('evil.invalid')) remoteRequests.push(request.url())
    })
    await page.goto(`/r/team-tools/skill/secure-review/revisions/${encodeURIComponent(digest)}`)

    await expect(page).toHaveURL(new RegExp(`/r/team-tools/skill/secure-review/revisions/${encodeURIComponent(digest)}$`))
    await expect(page.getByRole('heading', { name: 'team-tools/secure-review', exact: true })).toBeVisible()
    await expect(page.getByText(digest, { exact: true }).first()).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Revision history', exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: `sha256:${'9'.repeat(64)}`, exact: true })).toHaveAttribute('href', `/r/team-tools/skill/secure-review/revisions/sha256%3A${'9'.repeat(64)}`)
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
    await page.goto(`/r/team-tools/skill/secure-review/revisions/${encodeURIComponent(digest)}`)

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
    await page.goto(`/r/team-tools/command/deploy-preview/revisions/${encodeURIComponent(revokedDigest)}`)

    await expect(page.getByRole('heading', { name: 'team-tools/deploy-preview', exact: true })).toBeVisible()
    await expect(page.getByRole('alert')).toContainText('This revision is revoked')
    await expect(page.getByText(revokedDigest, { exact: true }).first()).toBeVisible()
    await expect(page.getByRole('button', { name: /Copy pinned CLI install command/ })).toBeDisabled()
    await expect(page.getByRole('button', { name: /Copy detail URL/ })).toBeEnabled()
  })

  test('shows the registry history empty state', async ({ page }) => {
    await page.goto('/registries/team-tools/history')

    await expect(page.getByRole('heading', { name: 'team-tools', exact: true })).toBeVisible()
    await expect(page.getByText('No registry occurrences have been recorded.', { exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Registry overview', exact: true })).toHaveAttribute('href', '/registries/team-tools')
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
    await page.getByLabel('Search', { exact: true }).fill('secure')
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
    await relativePage.goto(relativeAppBaseURL)
    await expect(relativePage.getByText('· 2 total', { exact: true })).toBeVisible()
    await relativePage.getByLabel('Search', { exact: true }).fill('secure')
    await expect(relativePage.getByText('· 1 total', { exact: true })).toBeVisible()
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
