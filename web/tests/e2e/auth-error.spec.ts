import { expect, test } from '@playwright/test'
import type { Page, Route } from '@playwright/test'

const appOrigin = `http://127.0.0.1:${process.env.E2E_RELATIVE_APP_PORT || '4322'}`
const base = `${appOrigin}/marketplace`
const evidenceDir = process.env.E2E_EVIDENCE_DIR || '/tmp/oidc-error-evidence'

async function handoff(page: Page, location: string): Promise<string[]> {
  const adminRequests: string[] = []
  await page.route('**/api/v1/admin/auth/**', (route: Route) => route.fulfill({ status: 303, headers: { Location: location } }))
  await page.route('**/api/v1/admin/session', (route: Route) => {
    adminRequests.push(route.request().url())
    return route.fulfill({ status: 401, json: { code: 'unauthenticated', message: 'nope' } })
  })
  await page.route('**/api/v1/admin/configuration', (route: Route) => {
    adminRequests.push(route.request().url())
    return route.fulfill({ status: 401, json: { code: 'unauthenticated', message: 'nope' } })
  })
  return adminRequests
}

// spec: marketplace-admin-auth/failure-page-renders-without-a-session
// spec: marketplace-admin-auth/error-presentation-is-private-and-accessible
test.describe('sign-in error page', () => {
  test('follows the login handoff into the explained failure without admin requests', async ({ page }) => {
    const adminRequests = await handoff(page, '/marketplace/auth/error?code=authentication_unavailable&next=%2Fmarketplace%2Fadmin%2Fregistries')
    const response = await page.goto(`${appOrigin}/api/v1/admin/auth/login?next=%2Fmarketplace%2Fadmin%2Fregistries`)
    expect(response?.status()).toBe(200)
    await expect(page).toHaveURL(`${base}/auth/error?code=authentication_unavailable&next=/marketplace/admin/registries`)
    await expect(page.getByRole('heading', { name: 'Sign-in is temporarily unavailable' })).toBeVisible()
    await expect(page.locator('#main-content')).toContainText('identity provider')
    await expect(page.getByRole('link', { name: /try signing in again/i })).toBeVisible()
    await expect(page.getByRole('link', { name: /return to the marketplace/i })).toHaveAttribute('href', '/marketplace/')
    expect(adminRequests).toEqual([])
    await expect(page.locator('#main-content')).not.toContainText('access_denied')
    await expect(page.locator('#main-content')).not.toContainText('error_description')
  })

  test('retry starts a fresh login with the validated destination, never the callback', async ({ page }) => {
    await page.route('**/api/v1/admin/auth/**', (route: Route) => route.fulfill({ status: 303, headers: { Location: '/marketplace/auth/error?code=authentication_rejected&next=%2Fmarketplace%2Fadmin%2Fregistries' } }))
    await page.goto(`${appOrigin}/api/v1/admin/auth/callback?state=x&code=c`)
    await expect(page.getByRole('heading', { name: "Sign-in wasn't completed" })).toBeVisible()
    const loginRequest = page.waitForRequest('**/api/v1/admin/auth/login**')
    await page.getByRole('link', { name: /try signing in again/i }).click()
    const target = new URL((await loginRequest).url())
    expect(target.pathname).toBe('/api/v1/admin/auth/login')
    expect(target.searchParams.get('next')).toBe('/marketplace/admin/registries')
    expect(target.toString()).not.toContain('callback')
  })

  test('unknown and malformed parameters fall back to the generic explanation', async ({ page }) => {
    for (const query of [
      'code=bogus-value&error_description=provider-secret',
      'code=authentication_rejected&code=forbidden',
      'next=https://evil.test/phish',
      '',
    ]) {
      await page.goto(`${base}/auth/error${query ? `?${query}` : ''}`)
      await expect(page.getByRole('heading', { name: "Sign-in couldn't be completed" }), query).toBeVisible()
      await expect(page.locator('#main-content')).not.toContainText('provider-secret')
      await expect(page.locator('#main-content')).not.toContainText('bogus-value')
      const retry = page.getByRole('link', { name: /try signing in again/i })
      await expect(retry).toHaveAttribute('href', '/api/v1/admin/auth/login?next=%2Fmarketplace%2Fadmin')
    }
  })

  test('non-retryable failures give operator guidance and a Marketplace return', async ({ page }) => {
    for (const [code, heading] of [
      ['forbidden', 'Administration access required'],
      ['admin_unavailable', 'Administration sign-in is not configured'],
      ['origin_rejected', 'Sign-in must start from the Marketplace'],
    ] as const) {
      await page.goto(`${base}/auth/error?code=${code}`)
      await expect(page.getByRole('heading', { name: heading }), code).toBeVisible()
      await expect(page.getByRole('link', { name: /try signing in again/i }), code).toHaveCount(0)
      await expect(page.getByRole('link', { name: /return to the marketplace/i }), code).toHaveAttribute('href', '/marketplace/')
    }
  })

  test('renders and stays private without JavaScript', async ({ browser }) => {
    const context = await browser.newContext({ javaScriptEnabled: false })
    const page = await context.newPage()
    const response = await page.goto(`${base}/auth/error?code=authentication_rejected`)
    expect(response).not.toBeNull()
    const headers = response!.headers()
    expect(headers['cache-control']).toContain('no-store')
    expect(headers['referrer-policy']).toBe('no-referrer')
    expect(headers['content-security-policy']).toBeTruthy()
    const html = await response!.text()
    expect(html).toContain('Sign-in wasn')
    expect(html).toContain('noindex')
    expect(html).toContain('/api/v1/admin/auth/login')
    expect(html).not.toContain('access_denied')
    await expect(page.locator('meta[name="robots"]')).toHaveAttribute('content', /noindex/)
    await expect(page.locator('#main-content')).toBeVisible()
    await context.close()
  })

  test('supports keyboard navigation and reduced motion', async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto(`${base}/auth/error?code=authentication_unavailable`)
    const retry = page.getByRole('link', { name: /try signing in again/i })
    await retry.focus()
    await expect(retry).toBeFocused()
    const outline = await page.evaluate(() => {
      const element = document.activeElement as HTMLElement | null
      return element ? getComputedStyle(element).outlineStyle : ''
    })
    expect(outline).not.toBe('none')
  })

  test('visual checkpoint desktop and mobile in light and dark', async ({ page }) => {
    for (const [viewport, scheme] of [
      [{ width: 1280, height: 800 }, 'light'],
      [{ width: 1280, height: 800 }, 'dark'],
      [{ width: 390, height: 844 }, 'light'],
      [{ width: 390, height: 844 }, 'dark'],
    ] as const) {
      await page.setViewportSize(viewport)
      await page.emulateMedia({ colorScheme: scheme })
      await page.addInitScript(mode => localStorage.setItem('vitepress-theme-appearance', mode), scheme)
      await page.goto(`${base}/auth/error?code=authentication_rejected&next=%2Fmarketplace%2Fadmin`)
      await expect(page.getByRole('heading', { name: "Sign-in wasn't completed" })).toBeVisible()
      await page.screenshot({ path: `${evidenceDir}/error-${viewport.width}x${viewport.height}-${scheme}.png`, fullPage: true })
    }
  })

  test('updates the explanation on a client-side route transition', async ({ page }) => {
    await page.goto(`${base}/auth/error?code=authentication_rejected`)
    await expect(page.getByRole('heading', { name: "Sign-in wasn't completed" })).toBeVisible()
    const transition = async (query: Record<string, string>) => page.evaluate(async (q) => {
      const host = document.querySelector('#__nuxt') as (HTMLElement & { __vue_app__?: { config: { globalProperties: { $router?: { replace(location: { query: Record<string, string> }): Promise<void> } } } } }) | null
      const router = host?.__vue_app__?.config.globalProperties.$router
      if (!router) {
        return false
      }
      await router.replace({ query: q })
      return true
    }, query)
    expect(await transition({ code: 'forbidden' })).toBe(true)
    await expect(page.getByRole('heading', { name: 'Administration access required' })).toBeVisible()
    await expect(page.getByRole('link', { name: /try signing in again/i })).toHaveCount(0)
    await expect(page.locator('#main-content')).toContainText('forbidden')
    expect(await transition({ code: 'bogus-value', error_description: 'provider-secret' })).toBe(true)
    await expect(page.getByRole('heading', { name: "Sign-in couldn't be completed" })).toBeVisible()
    await expect(page.locator('#main-content')).not.toContainText('provider-secret')
    await expect(page.locator('#main-content')).not.toContainText('bogus-value')
    await expect(page.getByRole('link', { name: /try signing in again/i })).toBeVisible()
  })
})
