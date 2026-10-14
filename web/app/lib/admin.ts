import type { components } from '~/generated/api'

export type AdminRegistry = components['schemas']['AdminRegistry']
export type AdminPolicy = components['schemas']['AdminLicensePolicy']
export type AdminDistributionPolicy = components['schemas']['AdminDistributionPolicy']
export type AdminConfiguration = components['schemas']['AdminConfiguration']
export type AdminProfiles = components['schemas']['AdminProfiles']
export type AdminSession = components['schemas']['AdminSession']
export type AdminInstance = components['schemas']['AdminInstance']
export type AdminProposal = components['schemas']['AdminProposal']
export type AdminChange = components['schemas']['AdminChange']
export type AdminJob = components['schemas']['AdminJob']
export type AdminAttempt = components['schemas']['AdminAttempt']
export type AdminPreview = components['schemas']['AdminPreview']
export type AdminPreviewResult = components['schemas']['AdminPreviewResult']
export type AdminPreviewResource = components['schemas']['AdminPreviewResource']
export type AdminPreviewRoot = AdminPreviewResult['roots'][number]

// A preview plus the exact draft it was requested for. The server echoes the
// proposal it stored, but the draft comparison uses this client-side snapshot so
// field ordering and defaults added by the server never mask a real edit.
export type AdminPreviewState = { preview: AdminPreview, registry: string }

// Sign-in destinations must be beneath the administration path for the
// configured Marketplace web base. Anything else falls back to that admin root
// rather than being reflected into the login redirect.
export function adminLoginHref(next: string, baseURL = '/marketplace/'): string {
  const root = `${baseURL.replace(/\/+$/, '')}/admin`
  const safeDestination = new TextEncoder().encode(next).byteLength <= 2048 && Array.from(next).every(char => {
    const code = char.charCodeAt(0)
    return code >= 0x21 && code !== 0x7f
  })
  const target = safeDestination && (next === root || next.startsWith(`${root}/`) && !/[\s\\?#%]|\.\./.test(next)) ? next : root
  return `/api/v1/admin/auth/login?next=${encodeURIComponent(target)}`
}

export class AdminError extends Error {
  constructor(public code: string, message: string, public fields: Record<string, string> = {}) {
    super(message)
  }
}

// Deliberately separate from the public SSR fetcher. Never forward identity
// headers, follow SSO redirects, retry mutations, or use a cross-origin base.
export function createAdminApi(fetcher: typeof fetch = fetch) {
  async function request<T>(path: string, proposal?: AdminProposal, revision?: number, emptyMutation = false): Promise<T> {
    if (typeof window === 'undefined') throw new AdminError('client_only', 'Administration is only available in the browser.')
    if (proposal?.action === 'upsert_registry' && proposal.registry) proposal = { ...proposal, registry_id: proposal.registry.id }
    const mutation = proposal !== undefined || emptyMutation
    const headers: Record<string, string> = { Accept: 'application/json' }
    if (mutation) {
      headers['Content-Type'] = 'application/json'
      headers['X-Marketplace-Admin'] = '1'
    }
    if (proposal) {
      headers['If-Match'] = `"${revision}"`
      headers['Idempotency-Key'] = crypto.randomUUID()
    }
    let response: Response
    try {
      response = await fetcher(`/api/v1/admin${path}`, {
        method: mutation ? 'POST' : 'GET', credentials: 'same-origin',
        cache: 'no-store', redirect: 'error', headers,
        ...(mutation ? { body: JSON.stringify(proposal ?? {}) } : {}),
      })
    }
    catch {
      throw new AdminError('network_error', 'Request outcome is unknown. Check Activity before preparing another change. Nothing will be replayed automatically.')
    }
    if (!response.headers.get('content-type')?.includes('application/json')) {
      throw new AdminError('invalid_response', 'The server did not return an admin response. Reauthenticate through your deployment and reload; no change will be replayed.')
    }
    let data
    try { data = await response.json() }
    catch { throw new AdminError('invalid_response', 'The response was not valid JSON. Check Activity before preparing another change; nothing is automatically replayed.') }
    if (!response.ok) {
      throw new AdminError(data.code || 'request_failed', data.message || 'Administration request failed.', data.fields || {})
    }
    return data as T
  }
  return {
    session: () => request<AdminSession>('/session'),
    configuration: () => request<AdminConfiguration>('/configuration'),
    profiles: () => request<AdminProfiles>('/profiles'),
    instance: () => request<AdminInstance>('/instance'),
    activity: (before?: number) => request<{ changes: AdminChange[] }>(`/activity?limit=25${before ? `&before=${before}` : ''}`),
    job: (id: string) => request<AdminJob>(`/jobs/${encodeURIComponent(id)}`),
    attempts: (registryID: string, before?: number) => request<{ attempts: AdminAttempt[], next_before: number }>(`/attempts?registry_id=${encodeURIComponent(registryID)}&limit=25${before ? `&before=${before}` : ''}`),
    validate: (proposal: AdminProposal, revision: number) => request<{ valid: true, warnings: string[] }>('/validate', proposal, revision),
    preview: (proposal: AdminProposal, revision: number) => request<AdminPreview>('/previews', proposal, revision),
    previewStatus: (id: string) => request<AdminPreview>(`/previews/${encodeURIComponent(id)}`),
    apply: (proposal: AdminProposal, revision: number) => request<AdminChange>('/changes', proposal, revision),
    // Idempotent local sign-out under the same mutation protections; never
    // retried and never redirected by the client.
    logout: () => request<{ code: string }>('/auth/logout', undefined, undefined, true),
  }
}

export function cloneAdmin<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T
}

export function newAdminRegistry(): AdminRegistry {
  return { id: '', slug: '', origin: '', ref: 'main', roots: [{ path: '.', source_harness: '' }], acquisition: 'index-only', publisher: {}, license: '', rejected_resources: '', aliases: [], historical_origins: [], generation: 0, lifecycle: 'active', paused: false, withdrawn: false, retired: false }
}

// A historical settings restore must never restore old lifecycle or erase identity history.
export function restoreRegistrySettings(current: AdminRegistry, previous: AdminRegistry): AdminRegistry {
  return { ...cloneAdmin(previous), id: current.id, aliases: cloneAdmin(current.aliases), historical_origins: cloneAdmin(current.historical_origins), generation: current.generation, lifecycle: current.lifecycle, paused: current.paused, withdrawn: current.withdrawn, retired: current.retired, non_license_overrides: cloneAdmin(current.non_license_overrides || {}) }
}

// Stable serialization of a registry draft for preview staleness checks.
export function registryDraftKey(registry: AdminRegistry | undefined): string {
  return JSON.stringify(registry ?? null)
}

export function previewIsStale(state: AdminPreviewState | undefined, registry: AdminRegistry | undefined): boolean {
  return !!state && state.registry !== registryDraftKey(registry)
}

export function adminDiff(before: unknown, after: unknown): { field: string, before: string, after: string }[] {
  const old = (before || {}) as Record<string, unknown>
  const next = (after || {}) as Record<string, unknown>
  return [...new Set([...Object.keys(old), ...Object.keys(next)])].sort().flatMap((field) => {
    const left = JSON.stringify(old[field]) ?? 'Not set'
    const right = JSON.stringify(next[field]) ?? 'Not set'
    return left === right ? [] : [{ field, before: left, after: right }]
  })
}

export const lifecycleActions: { action: AdminProposal['action'], label: string, description: string }[] = [
  { action: 'pause', label: 'Pause refresh', description: 'Stop refresh and publication work. Existing publication remains available subject to policy.' },
  { action: 'resume', label: 'Resume refresh', description: 'Resume refresh. This does not restore withdrawn content.' },
  { action: 'withdraw', label: 'Withdraw availability', description: 'Block server-side acquisition, including retained exact revisions. Installed copies are not removed.' },
  { action: 'restore', label: 'Restore availability', description: 'Remove only the admin withdrawal and enter paused state. Deployment revocations and license restrictions still apply; resume is a separate action.' },
  { action: 'retire', label: 'Retire registry', description: 'Retire an already withdrawn registry. No refresh or downloads; identity, history, withdrawal and installed copies are retained.' },
  { action: 'restore_retired', label: 'Restore retired registry', description: 'Return the retired registry to management in withdrawn state. Separate restore availability and resume refresh actions must follow.' },
  { action: 'reindex', label: 'Request reindex', description: 'Queue durable indexing work. Acceptance is not a successful publication.' },
]
