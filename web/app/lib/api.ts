import { RESOURCE_KINDS } from '~/types/marketplace'
import type {
  ApiFetcher,
  InspectionFile,
  ResourceKind,
  SearchParams,
} from '~/types/marketplace'
import { normalizeInspectionManifest, normalizeMarketplace, normalizeResourceRevision, normalizeResourceRevisions, normalizeSearchResult, normalizeRegistry, normalizeRegistryHistory, normalizeRegistrySummaries, normalizeWellKnown } from '~/lib/validation'

const API_VERSION_PATH = '/api/v1'
const WELL_KNOWN_PATH = '/.well-known/agent-marketplace.json'

export function normalizeApiBase(rawBase: string | undefined): string {
  const value = (rawBase ?? '/').trim()
  if (!value || value === '/') return ''
  if (hasUnsafeURLCharacters(value)) return ''

  if (/^https?:\/\//i.test(value)) {
    try {
      const url = new URL(value)
      if (url.username || url.password || url.search || url.hash || !url.hostname) return ''
      const pathname = trimTrailingSlashes(url.pathname)
      return `${url.origin}${pathname}`
    } catch {
      return ''
    }
  }

  if (value.startsWith('//') || (!value.startsWith('/') && value.includes(':'))) return ''
  const path = value.startsWith('/') ? value : `/${value}`
  return trimTrailingSlashes(path)
}

export function isAbsoluteApiBase(value: string | undefined): boolean {
  return /^https?:\/\//i.test((value ?? '').trim())
}

// The private base is evaluated only by the SSR process. It must never be
// passed to client-side code, where Nuxt only exposes runtimeConfig.public.
export function selectMarketplaceApiBase(isServer: boolean, publicBase: string | undefined, internalBase: string | undefined): string | undefined {
  return isServer && internalBase?.trim() ? internalBase : publicBase
}

export function selectMarketplaceFetcher(isServer: boolean, apiBase: string | undefined, requestFetcher: ApiFetcher, clientFetcher: ApiFetcher): ApiFetcher {
  return isServer && !isAbsoluteApiBase(apiBase) ? requestFetcher : clientFetcher
}

export function serializeSearchQuery(params: SearchParams): Record<string, string | number> {
  const query: Record<string, string | number> = {}
  if (params.q.trim()) query.q = params.q.trim()
  if (params.kind) query.kind = params.kind
  if (params.registry.trim()) query.registry = params.registry.trim()
  if (params.offset > 0) query.offset = params.offset
  if (params.limit > 0) query.limit = params.limit
  return query
}

export function apiPath(apiBase: string | undefined, path: string): string {
  const base = normalizeApiBase(apiBase)
  const apiRoot = base.endsWith(API_VERSION_PATH) ? base : `${base}${API_VERSION_PATH}`
  return joinURL(apiRoot, path)
}

export function wellKnownPath(apiBase: string | undefined): string {
  const base = normalizeApiBase(apiBase)
  const root = base.endsWith(API_VERSION_PATH)
    ? base.slice(0, -API_VERSION_PATH.length)
    : base
  return joinURL(root, WELL_KNOWN_PATH)
}

export function createMarketplaceApi(fetcher: ApiFetcher, apiBase?: string) {
  return {
    getWellKnown: () => fetcher<unknown>(wellKnownPath(apiBase)).then(normalizeWellKnown),
    getInstance: () => fetcher<unknown>(apiPath(apiBase, '/instance')).then(normalizeMarketplace),
    listRegistries: () => fetcher<unknown>(apiPath(apiBase, '/registries')).then(normalizeRegistrySummaries),
    getRegistry: (registry: string) => fetcher<unknown>(registryPath(apiBase, registry)).then(normalizeRegistry),
    getRegistryHistory: (registry: string) => fetcher<unknown>(registryHistoryPath(apiBase, registry)).then(normalizeRegistryHistory),
    search: (params: SearchParams) =>
      fetcher<unknown>(apiPath(apiBase, '/search'), {
        query: serializeSearchQuery(params),
      }).then(normalizeSearchResult),
    getResource: (registry: string, kind: ResourceKind, name: string) =>
      fetcher<unknown>(resourcePath(apiBase, registry, kind, name)).then(payload => normalizeResourceRevision(payload, { registry, kind, name })),
    listResourceRevisions: (registry: string, kind: ResourceKind, name: string) =>
      fetcher<unknown>(resourceRevisionsPath(apiBase, registry, kind, name)).then(payload => normalizeResourceRevisions(payload, { kind, name })),
    getResourceRevision: (registry: string, kind: ResourceKind, name: string, revision: string) =>
      fetcher<unknown>(resourceRevisionPath(apiBase, registry, kind, name, revision)).then(payload => normalizeResourceRevision(payload, { registry, kind, name, contentDigest: revision })),
    getResourceInspection: (registry: string, kind: ResourceKind, name: string, revision: string) =>
      fetcher<unknown>(resourceInspectionPath(apiBase, registry, kind, name, revision)).then(payload => normalizeInspectionManifest(payload, { registry, kind, name, contentDigest: revision })),
    getResourceInspectionContent: (registry: string, kind: ResourceKind, name: string, revision: string, mediaType: InspectionFile['media_type'] = 'text/plain') =>
      fetcher<string | Blob>(resourceInspectionContentPath(apiBase, registry, kind, name, revision), { responseType: isRasterMediaType(mediaType) ? 'blob' : 'text' }),
    getResourceInspectionFile: (registry: string, kind: ResourceKind, name: string, revision: string, digest: string, mediaType?: InspectionFile['media_type']) =>
      fetcher<string | Blob>(resourceInspectionFilePath(apiBase, registry, kind, name, revision, digest), { responseType: isRasterMediaType(mediaType) ? 'blob' : 'text' }),
  }
}

export function isRasterMediaType(value: string | undefined): boolean {
  return value === 'image/png' || value === 'image/jpeg' || value === 'image/gif' || value === 'image/webp'
}

export function isResourceKind(value: string): value is ResourceKind {
  return RESOURCE_KINDS.includes(value as ResourceKind)
}

export function registryPath(apiBase: string | undefined, registry: string): string {
  return apiPath(apiBase, `/registries/${encodeURIComponent(registry)}`)
}

export function registryHistoryPath(apiBase: string | undefined, registry: string): string {
  return apiPath(apiBase, `/registries/${encodeURIComponent(registry)}/history`)
}

export function resourcePath(apiBase: string | undefined, registry: string, kind: ResourceKind, name: string): string {
  return apiPath(apiBase, `/resources/${encodeURIComponent(registry)}/${encodeURIComponent(kind)}/${encodeURIComponent(name)}`)
}

export function resourceRevisionPath(apiBase: string | undefined, registry: string, kind: ResourceKind, name: string, revision: string): string {
  return apiPath(apiBase, `/resources/${encodeURIComponent(registry)}/${encodeURIComponent(kind)}/${encodeURIComponent(name)}/revisions/${encodeURIComponent(revision)}`)
}

export function resourceRevisionsPath(apiBase: string | undefined, registry: string, kind: ResourceKind, name: string): string {
  return apiPath(apiBase, `/resources/${encodeURIComponent(registry)}/${encodeURIComponent(kind)}/${encodeURIComponent(name)}/revisions`)
}

export function resourceInspectionPath(apiBase: string | undefined, registry: string, kind: ResourceKind, name: string, revision: string): string {
  return `${resourceRevisionPath(apiBase, registry, kind, name, revision)}/inspection`
}

export function resourceInspectionContentPath(apiBase: string | undefined, registry: string, kind: ResourceKind, name: string, revision: string): string {
  return `${resourceInspectionPath(apiBase, registry, kind, name, revision)}/content`
}

export function resourceInspectionFilePath(apiBase: string | undefined, registry: string, kind: ResourceKind, name: string, revision: string, digest: string): string {
  return `${resourceInspectionContentPath(apiBase, registry, kind, name, revision)}/${encodeURIComponent(digest)}`
}

function hasUnsafeURLCharacters(value: string): boolean {
  return [...value].some(char => {
    const code = char.charCodeAt(0)
    return code <= 0x1f || code === 0x7f || ' \'"`$;&|<>()[]{}*!?#\\'.includes(char)
  })
}

function trimTrailingSlashes(value: string): string {
  return value.replace(/\/+$/, '')
}

function joinURL(base: string, path: string): string {
  return `${base}${path.startsWith('/') ? path : `/${path}`}` || '/'
}
