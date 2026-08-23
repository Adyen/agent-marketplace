import type { components, operations } from '~/generated/api'

export const RESOURCE_KINDS = ['skill', 'command', 'subagent', 'hook', 'mcpserver', 'collection'] as const

export type ResourceKind = (typeof RESOURCE_KINDS)[number]
export const ALL_RESOURCE_KINDS = 'all' as const
export type ResourceKindFilter = ResourceKind | typeof ALL_RESOURCE_KINDS

type Schemas = components['schemas']
type JsonResponse<Operation extends keyof operations> = operations[Operation]['responses'] extends Record<200, { content: { 'application/json': infer Response } }>
  ? Response
  : never

export type Capabilities = Schemas['Capabilities']
export type WellKnown = JsonResponse<'getWellKnown'>
export type Marketplace = JsonResponse<'getInstance'>
export type SourceRoot = Schemas['SourceRoot']
export type SourcePublisher = Schemas['Publisher']

// Public source metadata only. The API deliberately omits credentials, policy
// internals, and refresh diagnostics; origins are already sanitized server-side.
export type Source = JsonResponse<'getSource'>

export type SourceOccurrence = Schemas['SourceOccurrence']

export type ResourceListItem = Schemas['ResourceListItem']
export type SearchResult = JsonResponse<'searchResources'>

type GeneratedResourceRevision = JsonResponse<'getResource'>

// Validation permits bounded extension fields returned by existing servers and
// normalizes them to safer browser-facing structures while retaining the
// generated required fields.
export type ResourceRevision = Omit<GeneratedResourceRevision, 'coordinate' | 'findings' | 'provenance' | 'revocation'> & {
  coordinate: Record<string, string>
  acquisition?: string
  findings?: unknown[]
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
