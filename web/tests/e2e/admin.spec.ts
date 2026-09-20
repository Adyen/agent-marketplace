import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

const base = `http://127.0.0.1:${process.env.E2E_RELATIVE_APP_PORT || '4322'}/marketplace`
const registry = { id: 'stable-team', slug: 'team', aliases: ['old-team'], historical_origins: [], origin: 'https://git.example/team', ref: 'main', roots: [{ path: '.agents', source_harness: '' }, { path: '.claude', source_harness: 'claude' }], acquisition: 'index-only', publisher: { name: 'Team' }, license: 'MIT', generation: 2, lifecycle: 'active', paused: false, withdrawn: false, retired: false }
const snapshot = { version: 1, policy_revision: 2, license_policy: { allowlist: ['MIT'], undeclared: 'reject' }, registries: [registry], source_fingerprint: 'fingerprint' }

const preview = { id: 'preview-1', marketplace_id: 'test', actor: { issuer: 'test-sso', subject: 'operator' }, base_revision: 2, registry, existing: true, job_id: 'managed-preview-preview-1', status: 'pending', created_at: '2026-09-18T10:00:00Z' }
const previewResult = {
  commit: '0123456789abcdef0123456789abcdef01234567', ref: 'v2', observed_at: '2026-09-18T10:00:04Z', policy_revision: 2, diff_base: 'pub-1', total_resources: 2,
  roots: [
    { path: '.agents', resource_count: 2, resources: [{ kind: 'skill', name: 'review', source_path: '.agents/skills/review/SKILL.md', decision: 'allow', change: 'unchanged' }, { kind: 'command', name: 'lint', source_path: '.agents/commands/lint.md', decision: 'allow', change: 'added' }] },
    { path: '.claude', source_harness: 'claude', resource_count: 0, resources: [] },
  ],
  removed: [{ kind: 'subagent', name: 'reviewer', change: 'removed' }],
}

const change = { marketplace_id: 'test', revision: 3, parent_revision: 2, actor: { issuer: 'test-sso', subject: 'operator' }, operation: 'upsert_registry', reason: 'Change ref', request_id: 'request-1', idempotency_key: 'key', request_digest: 'digest', snapshot, job_ids: ['job-1'], created_at: '2026-09-18T10:00:00Z' }

async function mockAdmin(page: Page, status = 200, code = '') {
  const mutations: { path: string, body: Record<string, unknown>, headers: Record<string, string> }[] = []
  await page.route('**/api/v1/admin/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname.replace('/api/v1/admin', '')
    let body: unknown
    if (request.method() === 'POST') mutations.push({ path, body: request.postDataJSON(), headers: request.headers() })
    if (path === '/session') body = { actor: { issuer: 'test-sso', subject: 'operator' }, authentication_mode: 'oidc', capabilities: ['manage'] }
    else if (path === '/configuration') body = { revision: 2, snapshot }
    else if (path === '/profiles') body = { profiles: [{ id: 'approved-profile', allowed_origins: ['https://git.example'] }], source_harnesses: ['claude', 'codex'] }
    else if (path === '/validate' || path === '/changes') {
      const proposal = request.postDataJSON()
      if (proposal.action === 'upsert_registry' && (!proposal.registry_id || proposal.registry_id !== proposal.registry?.id)) return route.fulfill({ status: 422, json: { code: 'invalid_configuration', message: 'Registry identity is required.' } })
      if (path === '/validate') return route.fulfill({ json: { valid: true, warnings: [] } })
      if (status !== 200) return route.fulfill({ status, json: { code, message: 'Rejected' } })
      body = change
    }
    else if (path === '/previews') {
      const proposal = request.postDataJSON()
      if (proposal.action !== 'upsert_registry' || proposal.registry_id !== proposal.registry?.id) return route.fulfill({ status: 422, json: { code: 'invalid_configuration', message: 'Only registry drafts can be previewed.' } })
      return route.fulfill({ status: 202, json: preview, headers: { 'Cache-Control': 'no-store' } })
    }
    else if (path === '/previews/preview-1') body = { ...preview, status: 'succeeded', completed_at: '2026-09-18T10:00:04Z', result: previewResult }
    else if (path === '/jobs/job-1') body = { id: 'job-1', kind: 'managed-reconcile', status: 'queued', attempt: 0, max_attempts: 5, created_at: '2026-09-18T10:00:00Z', updated_at: '2026-09-18T10:00:00Z' }
    else if (path === '/activity') body = { changes: [change] }
    else if (path === '/attempts') {
      expect(new URL(request.url()).searchParams.get('registry_id')).toBe('stable-team')
      body = { attempts: [{ sequence: 9, registry_id: 'stable-team', job_id: 'child-refresh', parent_job_id: 'job-1', attempt: 1, outcome: 'failed', operation: 'reconcile', configuration_revision: 3, registry_generation: 2, policy_revision: 2, recorded_at: '2026-09-18T10:01:00Z' }], next_before: 0 }
    }
    else if (path === '/instance') body = { name: 'Test instance', version: 'dev', management_mode: 'managed', revision: 2, worker_status: 'No recent worker activity' }
    else return route.fulfill({ status: 404, json: { code: 'not_found', message: 'Not found' } })
    return route.fulfill({ json: body, headers: { 'Cache-Control': 'no-store' } })
  })
  return mutations
}

async function prepareReview(page: Page) {
  await page.goto(`${base}/admin/registries/stable-team/configuration`)
  await page.getByRole('button', { name: 'Edit configuration', exact: true }).click()
  await page.getByLabel('Ref or exact commit').fill('v2')
  await page.getByLabel('Reason for change').fill('Change ref')
  await page.getByRole('button', { name: 'Continue to review', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Review changes' })).toBeVisible()
}

test.describe('Private administration', () => {
  test('has a private no-store shell under the configured browser base', async ({ page, request }) => {
    const response = await request.get(`${base}/admin`)
    expect(response.headers()['cache-control']).toContain('no-store')
    expect(response.headers()['x-robots-tag']).toContain('noindex')
    expect(await response.text()).not.toContain('stable-team')
    await mockAdmin(page)
    await page.goto(`${base}/admin`)
    await expect(page.getByRole('heading', { name: 'Registries', exact: true })).toBeVisible()
    await expect(page.locator('meta[name="robots"]')).toHaveAttribute('content', 'noindex, nofollow, noarchive')
    await expect(page.getByRole('link', { name: 'Policies', exact: true })).toHaveAttribute('href', '/marketplace/admin/policies')
  })

  test('navigates from the registry table to a registry and back through the breadcrumb', async ({ page }) => {
    await mockAdmin(page)
    await page.goto(`${base}/admin`)
    const row = page.getByRole('row', { name: /team/ })
    await expect(row.getByText('Active')).toBeVisible()
    await expect(page.getByText('Publication health')).toHaveCount(0)
    await row.getByRole('link', { name: 'team', exact: true }).click()
    await expect(page).toHaveURL(`${base}/admin/registries/stable-team/configuration`)
    await expect(page.getByRole('heading', { level: 1, name: 'team' })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Actions' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Pause refresh' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Withdraw availability' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Resume refresh' })).toHaveCount(0)
    await expect(page.getByText('Not reported')).toHaveCount(0)
    await page.getByRole('navigation', { name: 'Breadcrumb' }).getByRole('link', { name: 'Registries' }).click()
    await expect(page).toHaveURL(`${base}/admin/registries`)
    await expect(page.getByRole('heading', { name: 'Registries', exact: true })).toBeVisible()
  })

  test('requires explicit validation, review and apply; observes queued rather than claiming success', async ({ page }) => {
    const mutations = await mockAdmin(page)
    await prepareReview(page)
    expect(mutations.map(value => value.path)).toEqual(['/validate'])
    await expect(page.getByRole('listitem').filter({ hasText: 'Review' }).and(page.locator('[aria-current="step"]'))).toBeVisible()
    await expect(page.getByRole('cell', { name: '"v2"' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Apply change', exact: true })).toBeDisabled()
    await page.getByRole('checkbox', { name: /I have reviewed the diff/ }).check()
    await page.getByRole('button', { name: 'Apply change', exact: true }).click()
    await expect(page.getByRole('status')).toHaveText('Change accepted as revision 3')
    await expect(page.getByText(/Acceptance does not mean indexing succeeded/)).toBeVisible()
    const job = page.getByRole('listitem').filter({ hasText: 'managed-reconcile' })
    await expect(job.getByText('Queued')).toBeVisible()
    await expect(job.getByText('job-1', { exact: true })).toBeVisible()
    expect(mutations.map(value => value.path)).toEqual(['/validate', '/changes'])
    expect(mutations[1]?.headers['if-match']).toBe('"2"')
    expect(mutations[1]?.headers['x-marketplace-admin']).toBe('1')
    expect(mutations[1]?.body).toEqual(mutations[0]?.body)
  })

  test('previews discovery for the exact draft, flags empty roots, and carries the outcome into review', async ({ page }) => {
    const mutations = await mockAdmin(page)
    await page.goto(`${base}/admin/registries/stable-team/configuration`)
    await page.getByRole('button', { name: 'Edit configuration', exact: true }).click()
    await page.getByLabel('Ref or exact commit').fill('v2')
    const panel = page.getByRole('region', { name: 'Discovery preview' })
    await panel.getByRole('button', { name: 'Preview discovery', exact: true }).click()
    await expect(panel.getByText(/One root exposed no resources/)).toBeVisible()
    expect(mutations.map(value => value.path)).toEqual(['/previews'])
    expect(mutations[0]?.headers['if-match']).toBe('"2"')
    expect((mutations[0]?.body.registry as { ref: string }).ref).toBe('v2')
    await expect(panel.getByText('Nothing discoverable under this root', { exact: false })).toBeVisible()
    await expect(panel.getByText('lint', { exact: true })).toBeVisible()
    await expect(panel.getByText('Currently published but no longer discovered')).toBeVisible()
    await expect(panel.getByRole('button', { name: 'Preview again', exact: true })).toBeVisible()
    await page.getByLabel('Ref or exact commit').fill('v3')
    await expect(panel.getByText(/draft changed since this preview/)).toBeVisible()
    await page.getByLabel('Ref or exact commit').fill('v2')
    await expect(panel.getByText(/draft changed since this preview/)).toHaveCount(0)
    await page.getByLabel('Reason for change').fill('Change ref')
    await page.getByRole('button', { name: 'Continue to review', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Review changes' })).toBeVisible()
    await expect(page.getByText(/found 2 resources at commit 0123456789ab; 1 currently published resource would disappear/)).toBeVisible()
    expect(mutations.map(value => value.path)).toEqual(['/previews', '/validate'])
  })

  for (const [status, code] of [[401, 'unauthenticated'], [409, 'revision_conflict']] as const) {
    test(`does not replay a change after ${code}`, async ({ page }) => {
      const mutations = await mockAdmin(page, status, code)
      await prepareReview(page)
      await page.getByRole('checkbox', { name: /I have reviewed the diff/ }).check()
      await page.getByRole('button', { name: 'Apply change', exact: true }).click()
      await expect(page.getByRole('alert')).toContainText(code)
      await expect(page.getByRole('button', { name: 'Continue to review', exact: true })).toBeDisabled()
      expect(mutations.filter(value => value.path === '/changes')).toHaveLength(1)
      await expect(page.getByLabel('Ref or exact commit')).toHaveValue('v2')
    })
  }

  test('validates creation with the newly entered identity', async ({ page }) => {
    const mutations = await mockAdmin(page)
    await page.goto(`${base}/admin`)
    await page.getByRole('button', { name: 'Add registry', exact: true }).click()
    await page.getByLabel('Immutable registry ID', { exact: true }).fill('new-team')
    await page.getByLabel('Slug', { exact: true }).fill('new-team')
    await page.getByLabel('Git origin', { exact: true }).fill('https://git.example/new-team')
    await page.getByLabel('Reason for change').fill('Create registry')
    await page.getByRole('button', { name: 'Continue to review', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Review changes' })).toBeVisible()
    expect(mutations[0]?.body.registry_id).toBe('new-team')
  })

  test('validates historical registry restoration with its immutable identity', async ({ page }) => {
    const mutations = await mockAdmin(page)
    await page.goto(`${base}/admin/activity`)
    await expect(page.getByRole('heading', { name: 'Registry settings changed' })).toBeVisible()
    await page.getByRole('button', { name: 'team settings', exact: true }).click()
    await expect(page.getByLabel('Reason for change')).toHaveValue('Restore registry settings from revision 3')
    await page.getByRole('button', { name: 'Continue to review', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Review changes' })).toBeVisible()
    expect(mutations[0]?.body.registry_id).toBe('stable-team')
    expect(mutations[0]?.body.registry).toMatchObject({ aliases: ['old-team'], generation: 2, lifecycle: 'active' })
  })

  test('supports keyboard root reordering and guards unsaved navigation', async ({ page }) => {
    await mockAdmin(page)
    await page.goto(`${base}/admin/registries/stable-team/configuration`)
    await page.getByRole('button', { name: 'Edit configuration', exact: true }).click()
    await page.getByRole('button', { name: 'Move root 2 up', exact: true }).focus()
    await page.keyboard.press('Enter')
    await expect(page.getByLabel('Root 1 path')).toHaveValue('.claude')
    page.once('dialog', dialog => dialog.dismiss())
    await page.getByRole('link', { name: 'Policies', exact: true }).click()
    await expect(page).toHaveURL(`${base}/admin/registries/stable-team/configuration`)
    await expect(page.getByLabel('Root 1 path')).toHaveValue('.claude')
  })

  test('offers explicit sign-in on unauthenticated with the current validated route as next', async ({ page }) => {
    await page.route('**/api/v1/admin/**', route => route.fulfill({ status: 401, json: { code: 'unauthenticated', message: 'Sign in.', fields: { authentication_mode: 'oidc' } } }))
    await page.goto(`${base}/admin/registries/stable-team/configuration`)
    const signIn = page.getByRole('link', { name: 'Sign in with Okta' })
    await expect(signIn).toBeVisible()
    await expect(signIn).toHaveAttribute('href', `/api/v1/admin/auth/login?next=${encodeURIComponent('/marketplace/admin/registries/stable-team/configuration')}`)
    await expect(page.getByRole('button', { name: /Apply|Save|Edit/ })).toHaveCount(0)
  })

  test('directs trusted-proxy users to their deployment without an OIDC dead end', async ({ page }) => {
    await page.route('**/api/v1/admin/**', route => route.fulfill({ status: 401, json: { code: 'unauthenticated', message: 'Sign in.', fields: { authentication_mode: 'trusted_proxy' } } }))
    await page.goto(`${base}/admin`)
    await expect(page.getByRole('alert')).toContainText('deployment identity proxy')
    await expect(page.getByRole('link', { name: 'Sign in with Okta' })).toHaveCount(0)
  })

  test('does not offer local sign-out for a trusted-proxy session', async ({ page }) => {
    await page.route('**/api/v1/admin/**', (route) => {
      const path = new URL(route.request().url()).pathname.replace('/api/v1/admin', '')
      if (path === '/session') return route.fulfill({ json: { actor: { issuer: 'proxy-sso', subject: 'operator' }, authentication_mode: 'trusted_proxy', capabilities: [] } })
      if (path === '/configuration') return route.fulfill({ json: { revision: 2, snapshot } })
      if (path === '/profiles') return route.fulfill({ json: { profiles: [], source_harnesses: [] } })
      return route.fulfill({ status: 404, json: { code: 'not_found', message: 'Not found' } })
    })
    await page.goto(`${base}/admin`)
    await expect(page.getByText('operator', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Sign out' })).toHaveCount(0)
  })

  test('states forbidden without offering mutation controls', async ({ page }) => {
    await page.route('**/api/v1/admin/**', route => route.fulfill({ status: 403, json: { code: 'forbidden', message: 'Denied.' } }))
    await page.goto(`${base}/admin`)
    await expect(page.getByRole('alert')).toContainText('not authorized')
    await expect(page.getByRole('link', { name: 'Sign in with Okta' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: /Apply|Save|Edit|Add/ })).toHaveCount(0)
  })

  test('logs out through the protected endpoint and returns to the signed-out view', async ({ page }) => {
    let authenticated = true
    const logouts: Record<string, string>[] = []
    await page.route('**/api/v1/admin/**', async (route) => {
      const request = route.request()
      const path = new URL(request.url()).pathname.replace('/api/v1/admin', '')
      if (path === '/auth/logout' && request.method() === 'POST') {
        logouts.push(request.headers())
        authenticated = false
        return route.fulfill({ json: { code: 'signed_out' } })
      }
      if (!authenticated) return route.fulfill({ status: 401, json: { code: 'unauthenticated', message: 'Sign in.', fields: { authentication_mode: 'oidc' } } })
      let body: unknown
      if (path === '/session') body = { actor: { issuer: 'test-sso', subject: 'operator' }, display: 'Operator One', authentication_mode: 'oidc', capabilities: [] }
      else if (path === '/configuration') body = { revision: 2, snapshot }
      else if (path === '/profiles') body = { profiles: [], source_harnesses: [] }
      else return route.fulfill({ status: 404, json: { code: 'not_found', message: 'Not found' } })
      return route.fulfill({ json: body })
    })
    await page.goto(`${base}/admin`)
    await expect(page.getByText('Operator One')).toBeVisible()
    await page.getByRole('button', { name: 'Sign out' }).click()
    await expect(page.getByRole('alert')).toContainText('Sign in required')
    await expect(page.getByRole('link', { name: 'Sign in with Okta' })).toBeVisible()
    expect(logouts).toHaveLength(1)
    expect(logouts[0]?.['x-marketplace-admin']).toBe('1')
    expect(logouts[0]?.['content-type']).toContain('application/json')
  })

  test('keeps the authenticated state when sign-out cannot be confirmed', async ({ page }) => {
    let logoutCalls = 0
    await page.route('**/api/v1/admin/**', async (route) => {
      const request = route.request()
      const path = new URL(request.url()).pathname.replace('/api/v1/admin', '')
      if (path === '/auth/logout' && request.method() === 'POST') {
        logoutCalls++
        return route.fulfill({ status: 503, json: { code: 'authentication_unavailable', message: 'Unavailable.' } })
      }
      let body: unknown
      if (path === '/session') body = { actor: { issuer: 'test-sso', subject: 'operator' }, display: 'Operator One', authentication_mode: 'oidc', capabilities: [] }
      else if (path === '/configuration') body = { revision: 2, snapshot }
      else if (path === '/profiles') body = { profiles: [], source_harnesses: [] }
      else return route.fulfill({ status: 404, json: { code: 'not_found', message: 'Not found' } })
      return route.fulfill({ json: body })
    })
    await page.goto(`${base}/admin`)
    await page.getByRole('button', { name: 'Sign out' }).click()
    await expect(page.getByRole('alert')).toContainText('Sign-out could not be confirmed')
    await expect(page.getByText('Operator One')).toBeVisible()
    await expect(page.getByRole('link', { name: 'Sign in with Okta' })).toHaveCount(0)
    expect(logoutCalls).toBe(1)
  })

  test('shows all requested read surfaces without fabricated status', async ({ page }) => {
    await mockAdmin(page)
    await page.goto(`${base}/admin/registries/stable-team/findings`)
    await expect(page.getByText(/This is not evidence that the registry has no findings/)).toBeVisible()
    await page.getByRole('link', { name: 'Resources', exact: true }).click()
    await expect(page.getByRole('link', { name: 'Browse published resources' })).toBeVisible()
    await page.getByRole('link', { name: 'Indexing history', exact: true }).click()
    const attempt = page.getByRole('article').filter({ hasText: 'reconcile' }).first()
    await expect(attempt.getByText('Failed', { exact: true })).toBeVisible()
    await expect(attempt.getByRole('heading', { name: /reconcile · attempt 1/ })).toBeVisible()
    await expect(attempt.getByText('child-refresh', { exact: true })).toBeVisible()
    await expect(attempt.getByRole('button', { name: 'Copy Job ID child-refresh' })).toBeVisible()
    await expect(page.getByRole('button', { name: /their completion is separate from the registry indexing outcomes above/i })).toBeVisible()
    await page.getByRole('link', { name: 'Instance', exact: true }).click()
    await expect(page.getByText('No recent worker activity')).toBeVisible()
    await expect(page.getByRole('button', { name: /Apply|Save|Edit/ })).toHaveCount(0)
  })
})
