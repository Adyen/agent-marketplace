import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

const base = `http://127.0.0.1:${process.env.E2E_RELATIVE_APP_PORT || '4322'}/marketplace`
const registry = { id: 'stable-team', slug: 'team', aliases: ['old-team'], historical_origins: [], origin: 'https://git.example/team', ref: 'main', roots: [{ path: '.agents', source_harness: '' }, { path: '.claude', source_harness: 'claude' }], acquisition: 'index-only', publisher: { name: 'Team' }, license: 'MIT', generation: 2, lifecycle: 'active', paused: false, withdrawn: false, retired: false }
const snapshot = { version: 1, policy_revision: 2, license_policy: { allowlist: ['MIT'], undeclared: 'reject' }, registries: [registry], source_fingerprint: 'fingerprint' }

const change = { marketplace_id: 'test', revision: 3, parent_revision: 2, actor: { issuer: 'test-sso', subject: 'operator' }, operation: 'upsert_registry', reason: 'Change ref', request_id: 'request-1', idempotency_key: 'key', request_digest: 'digest', snapshot, job_ids: ['job-1'], created_at: '2026-09-18T10:00:00Z' }

async function mockAdmin(page: Page, status = 200, code = '') {
  const mutations: { path: string, body: Record<string, unknown>, headers: Record<string, string> }[] = []
  await page.route('**/api/v1/admin/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname.replace('/api/v1/admin', '')
    let body: unknown
    if (request.method() === 'POST') mutations.push({ path, body: request.postDataJSON(), headers: request.headers() })
    if (path === '/session') body = { actor: { issuer: 'test-sso', subject: 'operator' }, capabilities: ['manage'] }
    else if (path === '/configuration') body = { revision: 2, snapshot }
    else if (path === '/profiles') body = { profiles: [{ id: 'approved-profile', allowed_origins: ['https://git.example'] }], source_harnesses: ['claude', 'codex'] }
    else if (path === '/validate' || path === '/changes') {
      const proposal = request.postDataJSON()
      if (proposal.action === 'upsert_registry' && (!proposal.registry_id || proposal.registry_id !== proposal.registry?.id)) return route.fulfill({ status: 422, json: { code: 'invalid_configuration', message: 'Registry identity is required.' } })
      if (path === '/validate') return route.fulfill({ json: { valid: true, warnings: [] } })
      if (status !== 200) return route.fulfill({ status, json: { code, message: 'Rejected' } })
      body = change
    }
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
