import type {
  ApiFetcher,
  ResourceKind,
  SearchParams,
} from '~/types/marketplace'
import { normalizeResourceRevision, normalizeSearchResult, normalizeWellKnown } from '~/lib/validation'

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
      url.pathname = trimTrailingSlashes(url.pathname)
      return `${url.origin}${url.pathname}`
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

export function selectMarketplaceFetcher(isServer: boolean, apiBase: string | undefined, requestFetcher: ApiFetcher, clientFetcher: ApiFetcher): ApiFetcher {
  return isServer && !isAbsoluteApiBase(apiBase) ? requestFetcher : clientFetcher
}

export function serializeSearchQuery(params: SearchParams): Record<string, string | number> {
  const query: Record<string, string | number> = {}
  if (params.q.trim()) query.q = params.q.trim()
  if (params.kind) query.kind = params.kind
  if (params.source.trim()) query.source = params.source.trim()
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
    search: (params: SearchParams) =>
      fetcher<unknown>(apiPath(apiBase, '/search'), {
        query: serializeSearchQuery(params),
      }).then(normalizeSearchResult),
    getResource: (source: string, kind: ResourceKind, name: string) =>
      fetcher<unknown>(resourcePath(apiBase, source, kind, name)).then(payload => normalizeResourceRevision(payload, { source, kind, name })),
    getResourceRevision: (source: string, kind: ResourceKind, name: string, revision: string) =>
      fetcher<unknown>(resourceRevisionPath(apiBase, source, kind, name, revision)).then(payload => normalizeResourceRevision(payload, { source, kind, name, contentDigest: revision })),
  }
}

export function isResourceKind(value: string): value is ResourceKind {
  return ['skill', 'command', 'subagent', 'hook', 'mcpserver', 'collection'].includes(value)
}

export function resourcePath(apiBase: string | undefined, source: string, kind: ResourceKind, name: string): string {
  return apiPath(apiBase, `/resources/${encodeURIComponent(source)}/${encodeURIComponent(kind)}/${encodeURIComponent(name)}`)
}

export function resourceRevisionPath(apiBase: string | undefined, source: string, kind: ResourceKind, name: string, revision: string): string {
  return apiPath(apiBase, `/resources/${encodeURIComponent(source)}/${encodeURIComponent(kind)}/${encodeURIComponent(name)}/revisions/${encodeURIComponent(revision)}`)
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
