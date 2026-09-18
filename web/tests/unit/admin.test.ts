import { describe, expect, it, vi } from 'vitest'
import { AdminError, adminDiff, createAdminApi, newAdminRegistry, restoreRegistrySettings } from '../../app/lib/admin'

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

describe('private admin transport', () => {
  it('sends same-origin no-store requests with explicit concurrency and CSRF headers', async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(json({ revision: 4 }))
    const proposal = { action: 'reindex' as const, registry_id: 'team', reason: 'retry acquisition' }
    await createAdminApi(fetcher).apply(proposal, 3)
    expect(fetcher).toHaveBeenCalledTimes(1)
    const [url, options] = fetcher.mock.calls[0]!
    expect(url).toBe('/api/v1/admin/changes')
    expect(options).toMatchObject({ method: 'POST', cache: 'no-store', credentials: 'same-origin', redirect: 'error', body: JSON.stringify(proposal) })
    expect(options?.headers).toMatchObject({ 'If-Match': '"3"', 'X-Marketplace-Admin': '1', 'Content-Type': 'application/json' })
    expect((options?.headers as Record<string, string>)['Idempotency-Key']).toMatch(/^[a-f\d-]{36}$/)
    expect(Object.keys(options?.headers || {})).not.toContain('Authorization')
  })

  it.each([[401, 'unauthenticated'], [403, 'forbidden'], [409, 'revision_conflict'], [409, 'idempotency_conflict'], [422, 'invalid_configuration']])('never retries %s %s', async (status, code) => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(json({ code, message: 'Rejected', fields: { reason: 'Required' } }, Number(status)))
    await expect(createAdminApi(fetcher).apply({ action: 'pause', registry_id: 'team', reason: 'pause' }, 1)).rejects.toMatchObject({ code, fields: { reason: 'Required' } })
    expect(fetcher).toHaveBeenCalledTimes(1)
  })

  it('never automatically replays an ambiguous network outcome', async () => {
    const fetcher = vi.fn<typeof fetch>().mockRejectedValue(new TypeError('Failed to fetch'))
    await expect(createAdminApi(fetcher).apply({ action: 'pause', reason: 'pause' }, 1)).rejects.toMatchObject({ code: 'network_error' })
    expect(fetcher).toHaveBeenCalledTimes(1)
  })

  it('does not treat SSO HTML as a successful response', async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response('<html>SSO</html>', { headers: { 'Content-Type': 'text/html' } }))
    await expect(createAdminApi(fetcher).session()).rejects.toBeInstanceOf(AdminError)
    expect(fetcher).toHaveBeenCalledTimes(1)
  })

  it('never invokes a fetcher on the server', async () => {
    const fetcher = vi.fn<typeof fetch>()
    vi.stubGlobal('window', undefined)
    try { await expect(createAdminApi(fetcher).session()).rejects.toMatchObject({ code: 'client_only' }); expect(fetcher).not.toHaveBeenCalled() }
    finally { vi.unstubAllGlobals() }
  })
})

describe('review and historical restoration', () => {
  it('restores only settings while retaining current identity history and lifecycle', () => {
    const current = { ...newAdminRegistry(), id: 'stable-id', slug: 'current', aliases: ['old'], historical_origins: ['https://old.example/r'], lifecycle: 'withdrawn', paused: true, withdrawn: true, retired: false, generation: 9, non_license_overrides: { max_files: 99 } }
    const previous = { ...newAdminRegistry(), id: 'stable-id', slug: 'previous', origin: 'https://git.example/repo', aliases: [], lifecycle: 'active', generation: 1, non_license_overrides: { max_files: 1 } }
    const restored = restoreRegistrySettings(current, previous)
    expect(restored).toMatchObject({ id: 'stable-id', slug: 'previous', origin: 'https://git.example/repo', aliases: ['old'], historical_origins: ['https://old.example/r'], lifecycle: 'withdrawn', paused: true, withdrawn: true, retired: false, generation: 9, non_license_overrides: { max_files: 99 } })
    expect([restored.paused, restored.withdrawn, restored.retired]).toEqual([true, true, false])
    restored.aliases?.push('new')
    expect(current.aliases).toEqual(['old'])
    expect(previous.lifecycle).toBe('active')
  })

  it('shows additions, removals and changed values without unchanged noise', () => {
    expect(adminDiff({ ref: 'main', removed: 'old', equal: 4 }, { ref: 'v2', added: true, equal: 4 })).toEqual([
      { field: 'added', before: 'Not set', after: 'true' },
      { field: 'ref', before: '"main"', after: '"v2"' },
      { field: 'removed', before: '"old"', after: 'Not set' },
    ])
  })
})
