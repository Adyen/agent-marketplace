import { expect, test, type Page } from '@playwright/test'

const digest = `sha256:${'a'.repeat(64)}`
const notesDigest = `sha256:${'f'.repeat(64)}`
const configDigest = `sha256:${'1'.repeat(64)}`
const hostileDescription = 'Deploy safely & review <script>alert("xss")</script>'
const mockURL = `http://127.0.0.1:${process.env.E2E_MOCK_PORT || '4311'}`
const appURL = `http://127.0.0.1:${process.env.E2E_APP_PORT || '4321'}`
const relativeAppURL = `http://127.0.0.1:${process.env.E2E_RELATIVE_APP_PORT || '4322'}`
const relativeAppBaseURL = `${relativeAppURL}/marketplace/`
const sharedOriginURL = `http://127.0.0.1:${process.env.E2E_SHARED_ORIGIN_PORT || '4323'}`
const publicSiteURL = `http://127.0.0.1:${process.env.E2E_PUBLIC_SITE_PORT || '4326'}`
const sharedAppearanceKey = 'vitepress-theme-appearance'

function colorChannels(color: string): number[] {
  const channels = (color.match(/[0-9]*\.?[0-9]+/g) ?? []).slice(0, 3).map(Number)
  return channels.some(channel => channel > 1)
    ? channels.map(Math.round)
    : channels.map(channel => Math.round(channel * 255))
}

function contrastRatio(foreground: string, background: string): number {
  const luminance = (color: string) => {
    const [red, green, blue] = colorChannels(color).map((channel) => {
      const value = channel / 255
      return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4
    })
    return 0.2126 * red + 0.7152 * green + 0.0722 * blue
  }
  const [lighter, darker] = [luminance(foreground), luminance(background)].sort((a, b) => b - a)
  return (lighter + 0.05) / (darker + 0.05)
}

async function expectResourceCount(page: Page, count: number) {
  const label = `${count} ${count === 1 ? 'resource' : 'resources'}`
  await expect(page.locator('.catalog-results__header > p')).toContainText(label)
}

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

function streamingItems(count: number) {
  return Array.from({ length: count }, (_, index) => {
    const contentDigest = `sha256:${index.toString(16).padStart(64, '0')}`
    return {
      registry_id: 'team-tools',
      registry_slug: 'team-tools',
      kind: 'skill',
      name: `stream-result-${String(index + 1).padStart(2, '0')}`,
      revision: contentDigest,
      content_digest: contentDigest,
      description: `Streamed result ${index + 1}`,
    }
  })
}

async function routeStreamingSearch(page: Page, failNextPageOnce = false) {
  const items = streamingItems(30)
  const offsets: number[] = []
  const sorts: (string | null)[] = []
  let shouldFail = failNextPageOnce
  await page.route('**/api/v1/search**', async (route) => {
    const url = new URL(route.request().url())
    if (url.searchParams.get('q') !== 'stream') return route.continue()
    const offset = Number(url.searchParams.get('offset') || 0)
    const limit = Number(url.searchParams.get('limit') || 25)
    offsets.push(offset)
    sorts.push(url.searchParams.get('sort'))
    if (offset > 0 && shouldFail) {
      shouldFail = false
      return route.fulfill({ status: 400, contentType: 'application/json', body: '{}' })
    }
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ items: items.slice(offset, offset + limit), total: items.length, offset, limit }),
    })
  })
  return { offsets, sorts }
}

test.describe('Marketplace browser discovery', () => {
  // spec: marketplace-web-ui/footer-identifies-the-running-marketplace
  test('shows the running Marketplace version in the footer', async ({ page }) => {
    const instanceRequests: string[] = []
    page.on('request', (request) => {
      if (new URL(request.url()).pathname === '/api/v1/instance') instanceRequests.push(request.url())
    })
    await page.goto('/')
    const footer = page.locator('footer')
    await expect(footer).toContainText('Version v4.0.0-e2e')
    await expect(footer.getByRole('link', { name: 'Docs', exact: true })).toBeVisible()
    await expect(footer).toContainText('built at Adyen')
    expect(instanceRequests).toEqual([])
  })

  test('streams additional search results while scrolling', async ({ page }) => {
    const { offsets } = await routeStreamingSearch(page)
    await page.goto('/')
    await page.getByLabel('Search', { exact: true }).fill('stream')
    await expect(page.locator('article')).toHaveCount(25)
    await page.getByTestId('load-more-trigger').scrollIntoViewIfNeeded()
    await expect(page.locator('article')).toHaveCount(30)
    await expect(page.getByText('All 30 resources loaded.')).toBeVisible()
    await expect(page.getByRole('navigation', { name: 'Search results pagination' })).toHaveCount(0)
    expect(new URL(page.url()).searchParams.has('offset')).toBe(false)
    expect(offsets).toEqual([0, 25])
  })

  test('retains loaded results and offers a manual retry when streaming fails', async ({ page }) => {
    await page.addInitScript(() => {
      window.IntersectionObserver = class {
        observe() {}
        disconnect() {}
      } as unknown as typeof IntersectionObserver
    })
    const { offsets } = await routeStreamingSearch(page, true)
    await page.goto('/')
    await page.getByLabel('Search', { exact: true }).fill('stream')
    await expect(page.locator('article')).toHaveCount(25)
    await page.getByRole('button', { name: 'Load more resources' }).click()
    await expect(page.locator('article')).toHaveCount(25)
    await expect(page.getByRole('alert')).toContainText('Additional resources could not be loaded.')
    await page.getByRole('button', { name: 'Retry loading resources' }).click()
    await expect(page.locator('article')).toHaveCount(30)
    expect(offsets).toEqual([0, 25, 25])
  })

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

  // spec: marketplace-web-ui/marketplace-header-offers-non-redundant-external-documentation-navigation
  // spec: site-deployment-profiles/navigation-respects-deployment-context
  test('roots Marketplace navigation at the configured web base', async ({ browser, page }) => {
    await page.goto(appURL)
    const rootNavigation = page.getByRole('navigation', { name: 'Product navigation' })
    await expect(rootNavigation.getByRole('link', { name: 'agent-marketplace', exact: true })).toHaveAttribute('href', '/')
    await expect(rootNavigation.getByRole('link', { name: 'Docs', exact: true })).toHaveAttribute('href', 'https://agent-marketplace.io/docs')
    await expect(rootNavigation.getByRole('link', { name: 'Docs', exact: true })).toHaveAttribute('target', '_blank')
    await expect(rootNavigation.getByRole('link', { name: 'Docs', exact: true })).toHaveAttribute('rel', 'noopener noreferrer')
    await expect(rootNavigation.getByTestId('docs-external-icon')).toBeVisible()
    await expect(rootNavigation.getByRole('link', { name: 'Marketplace', exact: true })).toHaveCount(0)
    await expect(page.locator('footer').getByRole('link', { name: 'Docs', exact: true })).toHaveAttribute('href', 'https://agent-marketplace.io/docs')
    await page.goto(`${appURL}/r/team-tools/skill/secure-review`)
    await expect(page.getByRole('link', { name: /Set up CLI/ })).toHaveAttribute('href', 'https://agent-marketplace.io/docs')

    const relativePage = await browser.newPage()
    try {
      await relativePage.goto(relativeAppBaseURL)
      const relativeNavigation = relativePage.getByRole('navigation', { name: 'Product navigation' })
      await expect(relativeNavigation.getByRole('link', { name: 'agent-marketplace', exact: true })).toHaveAttribute('href', '/marketplace/')
      await expect(relativeNavigation.getByRole('link', { name: 'Docs', exact: true })).toHaveAttribute('href', '/docs/')
      await expect(relativeNavigation.getByRole('link', { name: 'Docs', exact: true })).toHaveAttribute('target', '_blank')
      await expect(relativeNavigation.getByTestId('docs-external-icon')).toBeVisible()
      await expect(relativeNavigation.getByRole('link', { name: 'Marketplace', exact: true })).toHaveCount(0)
    }
    finally {
      await relativePage.close()
    }
  })

  // spec: marketplace-web-ui/marketplace-links-to-locally-hosted-documentation-when-enabled
  test('opens bundled documentation without contacting the public website', async ({ context, page }) => {
    const externalRequests: string[] = []
    await context.route('**/*', async (route) => {
      const url = new URL(route.request().url())
      if (url.hostname !== '127.0.0.1' && url.hostname !== 'localhost') {
        externalRequests.push(url.href)
        await route.abort()
        return
      }
      await route.continue()
    })
    await page.goto(sharedOriginURL)
    const docs = page.getByRole('navigation', { name: 'Product navigation' }).getByRole('link', { name: 'Docs', exact: true })
    await expect(docs).toHaveAttribute('href', '/docs/')
    await expect(docs).toHaveAttribute('target', '_blank')
    await expect(docs).toHaveAttribute('rel', 'noopener noreferrer')
    await expect(page.locator('footer').getByRole('link', { name: 'Docs', exact: true })).toHaveAttribute('href', '/docs/')
    const popupPromise = page.waitForEvent('popup')
    await docs.click()
    const popup = await popupPromise
    try {
      await expect(popup).toHaveURL(`${sharedOriginURL}/docs/`)
      await expect(popup.getByRole('heading', { level: 1, name: /^Documentation\b/ })).toBeVisible()
      expect(await popup.evaluate(() => window.opener === null)).toBe(true)
      await popup.getByRole('link', { name: 'More install options', exact: true }).first().click()
      await expect(popup).toHaveURL(`${sharedOriginURL}/docs/install`)
    }
    finally {
      await popup.close()
    }
    await page.goto(`${sharedOriginURL}/r/team-tools/skill/secure-review`)
    await expect(page.getByRole('link', { name: /Set up CLI/ })).toHaveAttribute('href', '/docs/')
    expect(externalRequests).toEqual([])
  })

  test('keeps the public landing profile and its palette independent', async ({ page }) => {
    await page.goto(publicSiteURL)
    await expect(page.getByRole('heading', { name: /Manage AI agent tooling once/ })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Docs', exact: true }).first()).toHaveAttribute('href', '/docs/')
    await expect(page.getByRole('link', { name: 'Marketplace', exact: true })).toHaveCount(0)

    const lightHero = await page.locator('.hero').evaluate(element => getComputedStyle(element).getPropertyValue('--am-hero-bg').trim())
    await page.evaluate(() => document.documentElement.classList.add('dark'))
    const darkHero = await page.locator('.hero').evaluate(element => getComputedStyle(element).getPropertyValue('--am-hero-bg').trim())
    expect(lightHero).not.toBe('')
    expect(darkHero).not.toBe(lightHero)
  })

  // spec: site-deployment-profiles/marketplace-deployment-uses-root-ui-with-same-origin-services
  test('routes the self-hosted Marketplace UI, documentation, API, and discovery on one origin', async ({ page, request }) => {
    await page.goto(sharedOriginURL)
    await expect(page.getByRole('heading', { name: 'e2e-marketplace', exact: true })).toBeVisible()
    await expect(page.getByText('Manage AI agent tooling once', { exact: true })).toHaveCount(0)

    const [docs, instance, discovery] = await Promise.all([
      request.get(`${sharedOriginURL}/docs/`),
      request.get(`${sharedOriginURL}/api/v1/instance`),
      request.get(`${sharedOriginURL}/.well-known/agent-marketplace.json`),
    ])
    expect(docs.ok()).toBe(true)
    expect(await docs.text()).toMatch(/<h1[^>]*>Documentation\b/)
    expect(instance.ok()).toBe(true)
    expect((await instance.json()).name).toBe('E2E Marketplace')
    expect(discovery.ok()).toBe(true)
    expect((await discovery.json()).instance_id).toBe('e2e-marketplace')
  })

  // spec: marketplace-web-ui/browser-surfaces-present-accessible-adaptive-branding
  // spec: marketplace-web-ui/browser-surfaces-carry-the-approved-slash-motif
  test('shares navigation, appearance, and branding across browser surfaces', async ({ page }) => {
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

    await page.goto(publicSiteURL)
    await expect(page.locator('.h1-sub .am-brand-slash')).toHaveCount(0)
    const eyebrowSlash = page.locator('.eyebrow .am-brand-slash').first()
    await expect(eyebrowSlash).toHaveCSS('color', 'rgb(0, 209, 106)')
    await expect(eyebrowSlash).toHaveCSS('font-weight', '800')
    const eyebrowText = await page.locator('.eyebrow').first().evaluate(element => element.textContent ?? '')
    expect(eyebrowText).toMatch(/^\/\s\S/)
    const eyebrowColors = await page.locator('.eyebrow').first().evaluate((element) => ({
      label: getComputedStyle(element).color,
      slash: getComputedStyle(element.querySelector('.am-brand-slash')!).color,
    }))
    expect(eyebrowColors.label).not.toBe(eyebrowColors.slash)
    await expect(page.getByRole('heading', { name: /Manage AI agent tooling once Sync it everywhere you work/ })).toBeVisible()
    const landingBrand = page.locator('.VPNavBarTitle').getByRole('link', { name: 'agent-marketplace', exact: true })
    await expect(landingBrand).toHaveAttribute('href', '/')
    await expect(landingBrand.locator('img.light')).toHaveAttribute('src', '/brand/logo-horizontal-black.svg')
    await expect(landingBrand.locator('img.light')).toBeVisible()
    await expect(landingBrand.locator('img.dark')).toHaveAttribute('src', '/brand/logo-horizontal-white.svg')
    await expect(landingBrand.locator('img.dark')).toBeHidden()
    const sharedLogoWidth = await landingBrand.locator('img.light').evaluate(element => element.getBoundingClientRect().width)
    const sharedLogoTransform = await landingBrand.locator('img.light').evaluate(element => getComputedStyle(element).transform)
    expect(sharedLogoWidth).toBe(160)
    expect(sharedLogoTransform).not.toBe('none')
    await expect(page.locator('link[rel="icon"][type="image/svg+xml"]')).toHaveAttribute('href', '/brand/favicon.svg')

    await page.goto(sharedOriginURL)
    await page.evaluate(key => localStorage.setItem(key, 'light'), sharedAppearanceKey)
    await page.reload()

    const marketplaceNav = page.getByRole('navigation', { name: 'Product navigation' })
    const introSlash = page.locator('.marketplace-intro h1 .am-brand-slash')
    await expect(introSlash).toHaveText('/')
    await expect(introSlash).toHaveCSS('color', 'rgb(0, 209, 106)')
    await expect(introSlash).toHaveCSS('font-weight', '800')
    await expect(page.locator('.marketplace-intro h1')).toHaveText('/ e2e-marketplace')
    await expect(page.locator('#filters-heading .am-brand-slash')).toHaveCount(0)
    await expect(page.locator('#results-heading .am-brand-slash')).toHaveCount(0)
    await expect(page.getByRole('heading', { name: 'Filters', exact: true })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Resources', exact: true })).toBeVisible()
    const coordinateSlash = page.locator('.resource-coordinate__separator').first()
    await expect(coordinateSlash).toHaveText('/')
    await expect(coordinateSlash).toHaveCSS('color', 'rgb(0, 209, 106)')
    await expect(coordinateSlash).toHaveCSS('font-weight', '800')
    const marketplaceBrand = marketplaceNav.getByRole('link', { name: 'agent-marketplace', exact: true })
    await expect(marketplaceBrand).toHaveAttribute('href', '/')
    await expect(marketplaceBrand.locator('.am-site-nav__brand-logo--light')).toHaveAttribute('src', '/brand/logo-horizontal-black.svg')
    await expect(marketplaceBrand.locator('.am-site-nav__brand-logo--light')).toBeVisible()
    await expect(marketplaceBrand.locator('.am-site-nav__brand-logo--light')).toHaveCSS('width', `${sharedLogoWidth}px`)
    await expect(marketplaceBrand.locator('.am-site-nav__brand-logo--light')).toHaveCSS('transform', sharedLogoTransform)
    await expect(marketplaceBrand.locator('.am-site-nav__brand-logo--dark')).toHaveAttribute('src', '/brand/logo-horizontal-white.svg')
    await expect(marketplaceBrand.locator('.am-site-nav__brand-logo--dark')).toBeHidden()
    await expect(page.locator('link[rel="icon"][type="image/svg+xml"]')).toHaveAttribute('href', '/brand/favicon.svg')
    await expect(marketplaceNav.getByRole('link', { name: 'Marketplace', exact: true })).toHaveCount(0)
    await expect(marketplaceNav.getByRole('link', { name: 'Docs', exact: true })).toHaveAttribute('href', '/docs/')
    await expect(marketplaceNav.getByRole('link', { name: 'Docs', exact: true })).toHaveAttribute('target', '_blank')
    await expect(marketplaceNav.getByRole('link', { name: 'Docs', exact: true })).toHaveAttribute('rel', 'noopener noreferrer')
    await expect(marketplaceNav.getByTestId('docs-external-icon')).toBeVisible()
    await expect(page.locator('html')).not.toHaveClass(/\bdark\b/)
    await expect.poll(fontStatus).toEqual({
      bodyFamily: 'system-ui, sans-serif',
      fontSmoothing: 'antialiased',
      textRendering: 'optimizelegibility',
      downloadedFonts: 0,
    })

    const marketplaceSwitch = marketplaceNav.getByRole('switch', { name: 'Switch to dark theme' })
    await marketplaceSwitch.click()
    await expect(page.locator('html')).toHaveClass(/\bdark\b/)
    await expect(marketplaceBrand.locator('.am-site-nav__brand-logo--light')).toBeHidden()
    await expect(marketplaceBrand.locator('.am-site-nav__brand-logo--dark')).toBeVisible()
    await expect.poll(() => page.evaluate(key => localStorage.getItem(key), sharedAppearanceKey)).toBe('dark')

    await page.goto(`${sharedOriginURL}/docs/`)
    await expect(page.getByRole('heading', { name: /Documentation/ })).toBeVisible()
    const docsNav = page.getByRole('navigation', { name: 'Main Navigation' })
    const docsBrand = page.locator('.VPNavBarTitle').getByRole('link', { name: 'agent-marketplace', exact: true })
    await expect(docsBrand).toHaveAttribute('href', '/docs/')
    await expect(docsBrand.locator('img.light')).toHaveAttribute('src', '/docs/brand/logo-horizontal-black.svg')
    await expect(docsBrand.locator('img.light')).toBeHidden()
    await expect(docsBrand.locator('img.dark')).toHaveAttribute('src', '/docs/brand/logo-horizontal-white.svg')
    await expect(docsBrand.locator('img.dark')).toBeVisible()
    await expect(docsBrand.locator('img.dark')).toHaveCSS('width', `${sharedLogoWidth}px`)
    await expect(docsBrand.locator('img.dark')).toHaveCSS('transform', sharedLogoTransform)
    await expect(page.locator('link[rel="icon"][type="image/svg+xml"]')).toHaveAttribute('href', '/docs/brand/favicon.svg')
    await expect(docsNav.getByRole('link', { name: 'Marketplace', exact: true })).toHaveAttribute('href', '/')
    await expect(docsNav.getByRole('link', { name: 'Docs', exact: true })).toHaveAttribute('href', '/docs/')
    await expect(page.locator('html')).toHaveClass(/\bdark\b/)
    await expect.poll(() => page.evaluate(() => {
      const bodyStyle = getComputedStyle(document.body)
      return {
        bodyFamily: bodyStyle.fontFamily,
        fontSmoothing: bodyStyle.getPropertyValue('-webkit-font-smoothing'),
        textRendering: bodyStyle.textRendering,
        downloadedFonts: performance.getEntriesByType('resource')
          .filter(entry => /\.(?:woff2?|ttf|otf)(?:\?|$)/.test(entry.name))
          .length,
      }
    })).toEqual({
      bodyFamily: 'system-ui, sans-serif',
      fontSmoothing: 'antialiased',
      textRendering: 'optimizelegibility',
      downloadedFonts: 0,
    })

    const docsSwitch = page.locator('.VPNavBarAppearance .VPSwitchAppearance')
    await docsSwitch.click()
    await expect(page.locator('html')).not.toHaveClass(/\bdark\b/)
    await expect(docsBrand.locator('img.light')).toBeVisible()
    await expect(docsBrand.locator('img.dark')).toBeHidden()
    await expect.poll(() => page.evaluate(key => localStorage.getItem(key), sharedAppearanceKey)).toBe('auto')

    await docsNav.getByRole('link', { name: 'Marketplace', exact: true }).click()
    await expect(page).toHaveURL(`${sharedOriginURL}/`)
    await expect(page.getByRole('heading', { name: 'e2e-marketplace', exact: true })).toBeVisible()
    await expect(page.locator('html')).not.toHaveClass(/\bdark\b/)
  })

  test('keeps Nuxt color scales fixed and ordered in both themes', async ({ page }) => {
    const expected = {
      ink: [[243, 246, 244], [113, 135, 122], [20, 26, 24]],
      focus: [[240, 251, 244], [34, 184, 99], [16, 45, 28]],
      ready: [[240, 251, 244], [34, 184, 99], [16, 45, 28]],
      withheld: [[255, 251, 235], [245, 158, 11], [69, 26, 3]],
      revoked: [[254, 242, 242], [239, 68, 68], [69, 10, 10]],
    }
    const scales = Object.keys(expected)

    await page.goto('/')
    for (const preference of ['light', 'dark']) {
      await page.evaluate(
        ({ key, value }) => localStorage.setItem(key, value),
        { key: sharedAppearanceKey, value: preference },
      )
      await page.reload()

      const renderedScales = await page.evaluate((names) => {
        const probe = document.createElement('span')
        document.body.appendChild(probe)

        const result = Object.fromEntries(names.map((name) => {
          const shades = [50, 500, 950].map((shade) => {
            probe.style.color = `var(--color-${name}-${shade})`
            const rendered = getComputedStyle(probe).color
            const channels = (rendered.match(/[0-9]*\.?[0-9]+/g) ?? []).slice(0, 3).map(Number)
            return channels.some(channel => channel > 1)
              ? channels.map(Math.round)
              : channels.map(channel => Math.round(channel * 255))
          })
          return [name, shades]
        }))

        probe.remove()
        return result
      }, scales)

      for (const scale of scales) {
        expect(renderedScales[scale], `${scale} should not change in ${preference} mode`).toEqual(expected[scale])
      }
    }
  })

  test('applies the Marketplace palette in both themes', async ({ page }) => {
    const themes = {
      light: {
        marketplace: {
          canvas: 'rgb(255, 255, 255)',
          surface: 'rgb(255, 255, 255)',
          subtle: 'rgb(245, 246, 245)',
          raised: 'rgb(255, 255, 255)',
          chrome: 'rgb(255, 255, 255)',
          control: 'rgb(255, 255, 255)',
          hover: 'rgb(244, 245, 244)',
          code: 'rgb(245, 246, 245)',
          primaryText: 'rgb(27, 38, 32)',
          secondaryText: 'rgb(83, 97, 90)',
          mutedText: 'rgb(95, 110, 101)',
          border: 'rgb(222, 223, 222)',
          controlBorder: 'rgb(133, 136, 133)',
          action: 'rgb(8, 122, 61)',
          actionSoft: 'rgb(229, 243, 234)',
        },
      },
      dark: {
        marketplace: {
          canvas: 'rgb(20, 26, 24)',
          surface: 'rgb(27, 35, 32)',
          subtle: 'rgb(35, 46, 40)',
          raised: 'rgb(40, 52, 45)',
          chrome: 'rgb(27, 35, 32)',
          control: 'rgb(32, 42, 37)',
          hover: 'rgb(43, 56, 48)',
          code: 'rgb(20, 26, 24)',
          primaryText: 'rgb(237, 243, 239)',
          secondaryText: 'rgb(184, 196, 189)',
          mutedText: 'rgb(151, 168, 158)',
          border: 'rgb(54, 69, 60)',
          controlBorder: 'rgb(113, 135, 122)',
          action: 'rgb(85, 219, 137)',
          actionSoft: 'rgb(32, 61, 44)',
        },
      },
    } as const

    for (const preference of ['light', 'dark'] as const) {
      const theme = themes[preference]
      await page.goto('/')
      await page.evaluate(
        ({ key, value }) => localStorage.setItem(key, value),
        { key: sharedAppearanceKey, value: preference },
      )
      await page.reload()

      await expect(page.locator('body')).toHaveCSS('background-color', theme.marketplace.canvas)
      await expect(page.locator('main > section').first()).toHaveCSS('background-color', theme.marketplace.canvas)
      const resourcesSection = page.getByTestId('resources-section')
      await expect(resourcesSection).toHaveCSS('background-color', theme.marketplace.surface)
      await expect(page.getByTestId('search-results')).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)')
      const sectionGeometry = await page.evaluate(() => {
        const intro = document.querySelector('main > section:first-of-type')!.getBoundingClientRect()
        const resources = document.querySelector('[data-testid="resources-section"]')!.getBoundingClientRect()
        const footer = document.querySelector('footer')!.getBoundingClientRect()
        return {
          pageWidth: document.body.getBoundingClientRect().width,
          resources: { x: resources.x, y: resources.y, width: resources.width, bottom: resources.bottom },
          introBottom: intro.bottom,
          footerTop: footer.top,
        }
      })
      expect(sectionGeometry.resources.x).toBe(0)
      expect(sectionGeometry.resources.width).toBe(sectionGeometry.pageWidth)
      expect(Math.abs(sectionGeometry.resources.y - sectionGeometry.introBottom)).toBeLessThanOrEqual(1)
      expect(Math.abs(sectionGeometry.resources.bottom - sectionGeometry.footerTop)).toBeLessThanOrEqual(1)

      const header = page.locator('.am-site-header')
      await expect(header).toHaveCSS('background-color', theme.marketplace.chrome)
      await expect(header).toHaveCSS('border-bottom-color', theme.marketplace.border)

      const search = page.getByLabel('Search', { exact: true })
      await expect(search).toHaveCSS('background-color', theme.marketplace.control)
      await expect(search).toHaveCSS('border-color', theme.marketplace.controlBorder)
      await search.focus()
      await expect(search).toHaveCSS('outline-color', theme.marketplace.action)
      await expect(search).toHaveCSS('outline-width', '2px')

      const commandFilter = page.getByRole('radio', { name: 'Command', exact: true })
      await expect(commandFilter).toHaveCSS('accent-color', theme.marketplace.action)
      await commandFilter.check()
      await expect(page.getByRole('button', { name: 'Remove Command kind filter' })).toHaveCSS('background-color', theme.marketplace.subtle)

      await page.getByRole('button', { name: 'Clear', exact: true }).click()
      const row = page.getByTestId('search-results').locator('article').first()
      const namespace = row.locator('h2 span').first()
      const resourceName = row.locator('h2 a span.font-semibold')
      const description = row.locator('p.text-toned')
      await expect(namespace).toHaveCSS('color', theme.marketplace.mutedText)
      await expect(resourceName).toHaveCSS('color', theme.marketplace.primaryText)
      await expect(description).toHaveCSS('color', theme.marketplace.secondaryText)

      const coordinateLink = row.getByRole('link', { name: 'team-tools/secure-review', exact: true })
      await expect(coordinateLink).toHaveText('team-tools / secure-review')
      await expect(coordinateLink).toHaveCSS('box-shadow', 'none')
      await coordinateLink.hover()
      await expect.poll(() => coordinateLink.evaluate(element => getComputedStyle(element).boxShadow)).toContain('inset')
      await expect.poll(() => coordinateLink.evaluate(element => getComputedStyle(element).boxShadow)).toContain(theme.marketplace.action)
      await expect(row).toHaveCSS('background-color', theme.marketplace.hover)
      expect(contrastRatio(
        await namespace.evaluate(element => getComputedStyle(element).color),
        await row.evaluate(element => getComputedStyle(element).backgroundColor),
      )).toBeGreaterThanOrEqual(4.5)

      const copy = row.getByRole('button', { name: 'Copy install command for secure-review', exact: true })
      await expect(copy).toHaveCSS('background-color', theme.marketplace.subtle)
      await expect(copy).toHaveCSS('border-color', theme.marketplace.border)
      await expect(copy).toHaveCSS('color', theme.marketplace.primaryText)
      await copy.hover()
      await expect(copy).toHaveCSS('background-color', theme.marketplace.hover)
      await copy.focus()
      await page.keyboard.press('Shift+Tab')
      await page.keyboard.press('Tab')
      await expect(copy).toHaveCSS('outline-color', theme.marketplace.action)
      await expect(copy).toHaveCSS('outline-width', '2px')

      await expect(page.getByText('All 2 resources loaded.')).toHaveCSS('color', theme.marketplace.mutedText)

      await page.goto(`/r/team-tools/skill/secure-review/revisions/${encodeURIComponent(digest)}`)
      await expect(page.getByTestId('installation-panel')).toHaveCSS('border-top-width', '0px')
      await expect(page.locator('main > article header p.text-toned')).toHaveCSS('color', theme.marketplace.secondaryText)

      const inspectionSelect = page.getByLabel('Inspection file')
      await expect(inspectionSelect).toHaveCSS('background-color', theme.marketplace.control)
      await expect(inspectionSelect).toHaveCSS('border-color', theme.marketplace.controlBorder)
      await inspectionSelect.focus()
      await expect(inspectionSelect).toHaveCSS('outline-color', theme.marketplace.action)

      const markdown = page.getByTestId('safe-markdown')
      await expect(markdown.locator('code').first()).toHaveCSS('background-color', theme.marketplace.code)
      await expect(markdown.locator('code').first()).toHaveCSS('color', theme.marketplace.primaryText)
      await expect(markdown.locator('pre')).toHaveCSS('background-color', theme.marketplace.code)
      await expect(markdown.locator('pre')).toHaveCSS('border-color', theme.marketplace.border)

      const command = page.getByTestId('command-text')
      await expect(page.getByTestId('command-container')).toHaveCSS('background-color', theme.marketplace.code)
      await expect(command).toHaveCSS('color', theme.marketplace.primaryText)
      await expect(page.getByRole('button', { name: /Copy pinned CLI install command/ })).toHaveCSS('background-color', theme.marketplace.action)

      await expect(page.getByTestId('revision-summary')).toHaveCount(0)
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

    for (const path of ['/', '/docs/']) {
      await page.goto(`${sharedOriginURL}${path}`)
      const isDocs = path === '/docs/'
      const nav = page.getByRole('navigation', { name: isDocs ? 'Main Navigation' : 'Product navigation' })
      const menuButton = page.getByRole('button', { name: isDocs ? 'mobile navigation' : 'Toggle navigation' })
      const menuLink = isDocs
        ? page.locator('#VPNavScreen').getByRole('link', { name: 'Marketplace', exact: true })
        : nav.getByRole('link', { name: 'Docs', exact: true })

      const viewport = await page.evaluate(() => ({
        width: document.documentElement.clientWidth,
        scrollWidth: document.documentElement.scrollWidth,
      }))
      expect(viewport.scrollWidth).toBeLessThanOrEqual(viewport.width)

      await expect(menuButton).toBeVisible()
      await expect(menuButton).toHaveAttribute('aria-expanded', 'false')
      await expect(menuLink).not.toBeVisible()

      await menuButton.focus()
      await menuButton.press('Enter')
      await expect(menuButton).toHaveAttribute('aria-expanded', 'true')
      await expect(menuLink).toBeVisible()
      if (isDocs) {
        await menuButton.press('Enter')
      }
      else {
        await menuButton.press('Escape')
        await expect(menuButton).toBeFocused()
      }
      await expect(menuButton).toHaveAttribute('aria-expanded', 'false')
      await expect(menuLink).not.toBeVisible()
    }
  })

  test('explains direct resource installation without requiring Marketplace registration', async ({ page, context }) => {
    await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: appURL })
    await page.goto('/')

    await expect(page.getByRole('heading', { name: 'e2e-marketplace', exact: true })).toBeVisible()
    await expect(page.getByText('Browse resources for AI agents, with every revision pinned and analyzed before publication.', { exact: true })).toBeVisible()
    await expect(page.getByText(/To use a resource, install the agent-marketplace CLI, then copy and run its install command\./)).toBeVisible()
    await expect(page.getByText(`agent-marketplace install "${appURL}"`, { exact: true })).toHaveCount(0)
    const copyInstall = page.getByRole('button', { name: 'Copy install command for secure-review', exact: true })
    await copyInstall.click()
    await expect(page.getByRole('button', { name: 'Install command copied for secure-review', exact: true })).toHaveText('Copied')
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText()))
      .toBe(`agent-marketplace install "${appURL}/r/team-tools/skill/secure-review/revisions/${encodeURIComponent(digest)}"`)
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

  // spec: marketplace-browser/browser-displays-counts-while-preserving-filtered-pagination
  test('requests installation ordering and displays reported counts', async ({ page }) => {
    const searchRequests: URL[] = []
    page.on('request', request => {
      if (new URL(request.url()).pathname === '/api/v1/search') searchRequests.push(new URL(request.url()))
    })

    await page.goto('/')
    const rows = page.locator('article')
    await expect(rows).toHaveCount(2)
    await expect(rows.nth(0).getByRole('link', { name: 'team-tools/secure-review', exact: true })).toBeVisible()
    const countText = rows.nth(0).getByText('7 reported installations', { exact: true })
    await expect(countText).toBeVisible()
    await expect(countText).toHaveAttribute('title', /latest configuration within the last 30 days/)
    await expect(rows.nth(1)).not.toContainText('reported installation')

    await page.getByLabel('Search', { exact: true }).fill('secure')
    await expectResourceCount(page, 1)
    await expect(rows).toHaveCount(1)
    await expect(rows.nth(0)).toContainText('7 reported installations')

    await page.getByLabel('Search', { exact: true }).fill('')
    await page.getByRole('radio', { name: 'Command', exact: true }).check()
    await expectResourceCount(page, 1)
    await expect(rows.nth(0).getByRole('link', { name: 'team-tools/deploy-preview', exact: true })).toBeVisible()
    await expect(rows.nth(0)).not.toContainText('reported installation')

    await page.getByRole('button', { name: 'Clear', exact: true }).click()
    await page.getByRole('checkbox', { name: 'team-tools', exact: true }).check()
    await expectResourceCount(page, 2)
    await expect(rows.nth(0)).toContainText('7 reported installations')

    expect(searchRequests.length).toBeGreaterThan(0)
    for (const request of searchRequests) {
      expect(request.searchParams.get('sort')).toBe('installations')
    }
  })

  // spec: marketplace-browser/browser-displays-counts-while-preserving-filtered-pagination
  test('keeps installation ordering while loading more results', async ({ page }) => {
    const { offsets, sorts } = await routeStreamingSearch(page)
    await page.goto('/')
    await page.getByLabel('Search', { exact: true }).fill('stream')
    await expect(page.locator('article')).toHaveCount(25)
    await page.getByTestId('load-more-trigger').scrollIntoViewIfNeeded()
    await expect(page.locator('article')).toHaveCount(30)
    expect(offsets).toEqual([0, 25])
    expect(sorts).toEqual(['installations', 'installations'])
  })

  test('searches from the homepage and persists URL-backed filters', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByRole('heading', { name: 'Resources', exact: true })).toBeVisible()

    await page.getByLabel('Search', { exact: true }).fill('secure')
    await expect(page).toHaveURL(/q=secure/)
    await page.getByRole('checkbox', { name: 'team-tools', exact: true }).check()
    await page.getByRole('checkbox', { name: 'community-tools', exact: true }).check()

    await expect(page).toHaveURL(/registry=team-tools&registry=community-tools/)
    await expect(page.getByRole('button', { name: 'Remove team-tools registry filter' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Remove community-tools registry filter' })).toBeVisible()
    await expectResourceCount(page, 1)
    await expect(page.getByRole('link', { name: 'team-tools/secure-review', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Search', exact: true })).toHaveCount(0)

    await page.getByRole('button', { name: 'Clear', exact: true }).click()
    await expect(page.getByLabel('Search', { exact: true })).toHaveValue('')
    await expect(page).not.toHaveURL(/[?&]q=/)
  })

  test('filters by kind and exposes an all-kinds radio option', async ({ page }) => {
    const response = await page.goto('/')
    expect(response?.status()).toBe(200)
    await expect(page.getByRole('radio', { name: 'All kinds', exact: true })).toBeChecked()

    await page.getByRole('radio', { name: 'Command', exact: true }).check()
    await expect(page).toHaveURL(/kind=command/)
    await expectResourceCount(page, 1)
    await expect(page.getByRole('button', { name: 'Remove Command kind filter' })).toBeVisible()

    await page.getByRole('button', { name: 'Clear', exact: true }).click()
    await expect(page).not.toHaveURL(/[?&]kind=/)
    await expectResourceCount(page, 2)
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

  test('follows an exact resource link and exposes the same install pin', async ({ page, context }) => {
    await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: appURL })
    const previousDigest = `sha256:${'9'.repeat(64)}`
    await page.goto('/')
    await page.getByRole('link', { name: 'team-tools/secure-review', exact: true }).click()

    await expect(page).toHaveURL(`${appURL}/r/team-tools/skill/secure-review/revisions/${encodeURIComponent(digest)}`)
    await expect(page.getByRole('heading', { name: 'secure-review', exact: true })).toBeVisible()
    await expect(page.getByTestId('resource-title-slash')).toHaveText('/')
    await expect(page.getByTestId('resource-title-slash')).toHaveCSS('color', 'rgb(0, 209, 106)')
    await expect(page.getByTestId('resource-title-slash')).toHaveCSS('font-weight', '800')
    await expect(page.getByTestId('revision-summary')).toHaveCount(0)
    await expect(page.getByRole('heading', { name: 'Install', exact: true })).toBeVisible()
    const setupCLI = page.getByRole('link', { name: 'Set up CLI (opens in a new tab)', exact: true })
    await expect(setupCLI).toHaveAttribute('target', '_blank')
    await expect(setupCLI.getByTestId('setup-external-icon')).toHaveCount(1)
    const pinnedCommand = `agent-marketplace install "${appURL}/r/team-tools/skill/secure-review/revisions/${encodeURIComponent(digest)}"`
    await expect(page.getByTestId('command-text')).toHaveText(pinnedCommand)
    const copyCommand = page.getByRole('button', { name: /Copy pinned CLI install command/ })
    await expect(copyCommand).toBeEnabled()
    await copyCommand.click()
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(pinnedCommand)
    const revisionRow = page.getByTestId('revision-row')
    const copyRevision = revisionRow.getByRole('button')
    await expect(revisionRow.getByTestId('revision-label')).toHaveText('Revision (latest)')
    await expect(revisionRow.getByTestId('revision-id')).toHaveText('a'.repeat(12))
    await expect(revisionRow.getByTestId('scanned-group')).toHaveCount(0)
    await expect(revisionRow.getByTestId('source-link')).toHaveText('Adyen/agent-skills')
    await expect(revisionRow.getByTestId('source-link')).toHaveAttribute('href', `https://github.com/Adyen/agent-skills/blob/${'8'.repeat(40)}/skills/secure-review/SKILL.md`)
    await expect(revisionRow.getByTestId('source-path')).toHaveText('skills/secure-review/SKILL.md')
    await expect(copyRevision).toHaveAttribute('aria-label', /Copy revision/)
    await expect(copyRevision).toBeEnabled()
    await copyRevision.click()
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(digest)
    await expect(copyRevision).toHaveText('')
    await expect(copyRevision).toHaveAttribute('aria-label', 'Revision copied')
    await expect(revisionRow.getByTestId('revision-copy-status')).toHaveText('Copied')
    await expect(page.getByRole('button', { name: /Copy detail URL/ })).toHaveCount(0)

    await page.getByRole('tab', { name: 'Security', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Automated scan', exact: true })).toBeVisible()
    await expect(page.getByText('Scanned Jan 2, 2026', { exact: true })).toBeVisible()
    await page.getByText('Analyzer and assessment parameters', { exact: true }).click()
    await expect(page.getByText('e2e-analyzer-2', { exact: true })).toBeVisible()
    await expect(page.getByText(`${'6'.repeat(12)}…`, { exact: true })).toBeVisible()
    const copyAssessment = page.getByRole('button', { name: 'Copy Assessment digest', exact: true })
    await copyAssessment.click()
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(`sha256:${'6'.repeat(64)}`)
    await expect(page.getByText('remote-reference', { exact: true })).toBeVisible()

    await page.getByRole('tab', { name: 'Revisions', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Revision history', exact: true })).toBeVisible()
    const previousRevision = page.locator(`a[href="/r/team-tools/skill/secure-review/revisions/${encodeURIComponent(previousDigest)}?tab=revisions"]`)
    await expect(previousRevision).toBeVisible()
    const exactRevisionResponse = page.waitForResponse(response => response.url().includes(`/revisions/${encodeURIComponent(previousDigest)}`) && !response.url().includes('/inspection'))
    await previousRevision.click()
    expect((await exactRevisionResponse).status()).toBe(200)
    await expect(page).toHaveURL(`${appURL}/r/team-tools/skill/secure-review/revisions/${encodeURIComponent(previousDigest)}?tab=revisions`)
    await expect(page.getByRole('tab', { name: 'Revisions', exact: true })).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByRole('link', { name: 'Newer revision available', exact: true })).toHaveAttribute('href', `/r/team-tools/skill/secure-review/revisions/${encodeURIComponent(digest)}`)
    await expect(page.getByTestId('unavailable-notice')).toContainText('This revision has been revoked and can no longer be installed.')
  })

  test('follows an exact digest-pinned browser route to safely render hostile primary Markdown', async ({ page }) => {
    const remoteRequests: string[] = []
    page.on('request', request => {
      if (request.url().includes('evil.invalid')) remoteRequests.push(request.url())
    })
    await page.goto(`/r/team-tools/skill/secure-review/revisions/${encodeURIComponent(digest)}`)

    await expect(page).toHaveURL(new RegExp(`/r/team-tools/skill/secure-review/revisions/${encodeURIComponent(digest)}$`))
    await expect(page.getByRole('heading', { name: 'secure-review', exact: true })).toBeVisible()
    await expect(page.getByLabel('Inspection file')).toHaveValue(`sha256:${'e'.repeat(64)}`)
    await expect(page.getByRole('heading', { name: 'Safe review', exact: true })).toBeVisible()
    await expect(page.locator('[data-testid="safe-markdown"] a')).toHaveAttribute('href', 'https://example.com/docs')
    await expect(page.locator('[data-testid="safe-markdown"] a')).toHaveAttribute('target', '_blank')
    await expect(page.locator('[data-testid="safe-markdown"] a')).toHaveAttribute('rel', 'noopener noreferrer')
    const paragraphSpacing = await page.locator('[data-testid="safe-markdown"] p').first().evaluate(element => Number.parseFloat(getComputedStyle(element).marginBottom))
    expect(paragraphSpacing).toBeGreaterThan(0)
    await expect(page.locator('[data-testid="safe-markdown"] img')).toHaveCount(0)
    await expect(page.locator('[data-testid="safe-markdown"] script')).toHaveCount(0)
    await expect(page.locator('[data-testid="safe-markdown"]')).not.toContainText('evil.invalid')

    await page.getByRole('button', { name: 'Raw', exact: true }).click()
    await expect(page.locator('[data-testid="escaped-raw"]')).toContainText('<script>alert("xss")</script>')
    await expect(page.locator('[data-testid="escaped-raw"] mark')).toHaveAttribute('aria-label', /remote-reference \(warn\)/)

    await page.getByRole('tab', { name: 'Security', exact: true }).click()
    await expect(page.getByText('Severity: warn', { exact: true })).toBeVisible()
    await expect(page.getByText('Policy: network-policy', { exact: true })).toBeVisible()
    await page.getByRole('tab', { name: 'Revisions', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Revision history', exact: true })).toBeVisible()
    await expect(page.locator(`a[href="/r/team-tools/skill/secure-review/revisions/sha256%3A${'9'.repeat(64)}?tab=revisions"]`)).toBeVisible()
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

  test('keeps revoked revision metadata visible with one compact unavailable notice', async ({ page }) => {
    const revokedDigest = `sha256:${'b'.repeat(64)}`
    await page.goto(`/r/team-tools/command/deploy-preview/revisions/${encodeURIComponent(revokedDigest)}`)

    await expect(page.getByRole('heading', { name: 'deploy-preview', exact: true })).toBeVisible()
    const notice = page.getByTestId('unavailable-notice')
    await expect(notice).toContainText('Not available')
    await expect(notice).toContainText('This revision has been revoked and can no longer be installed.')
    await expect(notice.getByTestId('unavailable-status-icon')).toHaveCount(1)
    await expect(notice).toHaveCSS('border-top-width', '1px')
    expect(await notice.evaluate(element => getComputedStyle(element).backgroundColor)).not.toBe('rgba(0, 0, 0, 0)')
    await expect(page.getByRole('alert')).toHaveCount(0)
    await expect(page.getByRole('button', { name: /Copy pinned CLI install command/ })).toHaveCount(0)
    await expect(page.getByRole('link', { name: /Set up CLI/ })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'View security details', exact: true })).toHaveCount(0)
    await expect(page.getByTestId('revision-summary')).toHaveCount(0)
    await page.getByRole('tab', { name: 'Technical details', exact: true }).click()
    await expect(page.getByText(revokedDigest, { exact: true }).first()).toBeVisible()
    await expect(page.getByRole('button', { name: /Copy detail URL/ })).toHaveCount(0)
  })

  test('shows registry details in the resource tab and redirects the standalone route', async ({ page }) => {
    await page.goto('/r/team-tools/skill/secure-review')

    const breadcrumbs = page.getByRole('navigation', { name: 'Breadcrumb' })
    await expect(breadcrumbs.getByText('team-tools/secure-review', { exact: true })).toBeVisible()
    await expect(breadcrumbs.getByRole('link', { name: 'team-tools', exact: true })).toHaveCount(0)
    await page.getByRole('tab', { name: 'Registry', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'team-tools', exact: true })).toBeVisible()
    await expect(page.getByText('Platform Team', { exact: true })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Technical details', exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: 'View resources', exact: true })).toHaveAttribute('href', '/?registry=team-tools')

    await page.goto('/registries/team-tools')
    await expect(page).toHaveURL(`${appURL}/?registry=team-tools`)
  })

  test('shows the registry history empty state', async ({ page }) => {
    await page.goto('/registries/team-tools/history')

    await expect(page.getByRole('heading', { name: 'team-tools', exact: true })).toBeVisible()
    await expect(page.getByText('No registry occurrences have been recorded.', { exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Back to registry', exact: true })).toHaveAttribute('href', '/?registry=team-tools')
  })

  test('supports skip-link and form keyboard navigation', async ({ page }) => {
    await page.goto('/')
    await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur())
    await page.keyboard.press('Tab')
    await expect(page.locator('a[href="#main-content"]')).toBeFocused()
    await page.keyboard.press('Enter')
    await expect(page.locator('#main-content')).toBeFocused()

    const allKinds = page.getByRole('radio', { name: 'All kinds', exact: true })
    await allKinds.focus()
    await expect(allKinds).toBeFocused()
    await page.keyboard.press('ArrowDown')
    await expect(page.getByRole('radio', { name: 'Skill', exact: true })).toBeFocused()
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
    await expectResourceCount(relativePage, 2)
    await relativePage.getByLabel('Search', { exact: true }).fill('secure')
    await expectResourceCount(relativePage, 1)
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
