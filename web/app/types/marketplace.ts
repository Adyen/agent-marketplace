export const RESOURCE_KINDS = ['skill', 'command', 'subagent', 'hook', 'mcpserver', 'collection'] as const

export type ResourceKind = (typeof RESOURCE_KINDS)[number]

export interface Capabilities {
  api_versions: string[]
  artifact_schema_versions: string[]
  resource_kinds: string[]
  acquisition_modes: string[]
  features: string[]
}

export interface WellKnown extends Capabilities {
  instance_id: string
  canonical_url: string
  api_base: string
  browser_route_template: string
}

export interface Marketplace {
  id: string
  name: string
  canonical_url: string
  instance_metadata?: Record<string, unknown>
}

export interface SourceRoot {
  path: string
  source_harness?: string
}

export interface SourcePublisher {
  name: string
  url?: string
}

export interface Source {
  id: string
  slug: string
  aliases?: string[]
  origins: string[]
  ref: string
  roots: SourceRoot[]
  acquisition: 'index-only' | 'mirror'
  publisher?: SourcePublisher
  license?: string
  listed: boolean
}

export interface SourceOccurrence {
  source_id: string
  ref: string
  commit: string
  source_revision_manifest_digest: string
  acquisition: 'index-only' | 'mirror'
  mirrored_artifact_digest?: string
  occurred_at: string
}

export interface ResourceListItem {
  source_id: string
  source_slug?: string
  kind: ResourceKind
  name: string
  revision: string
  content_digest: string
  description?: string
  metadata?: Record<string, string>
  score?: number
}

export interface SearchResult {
  items: ResourceListItem[]
  total: number
  offset: number
  limit: number
}

export interface ResourceRevision {
  coordinate: Record<string, string>
  content_digest: string
  archive_digest?: string
  analyzer_version?: string
  acquisition?: string
  findings?: unknown[]
  license?: string
  provenance?: Record<string, unknown>
  revocation?: Record<string, unknown>
}

export interface SearchParams {
  q: string
  kind: ResourceKind | ''
  source: string
  offset: number
  limit: number
}

export interface FetchOptions {
  query?: Record<string, string | number>
}

export type ApiFetcher = <T>(request: string, options?: FetchOptions) => Promise<T>
