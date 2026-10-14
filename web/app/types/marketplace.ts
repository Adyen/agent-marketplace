import type { components, operations } from '~/generated/api'

export const RESOURCE_KINDS = ['skill', 'command', 'subagent', 'hook', 'mcpserver', 'collection'] as const

export type ResourceKind = (typeof RESOURCE_KINDS)[number]

type Schemas = components['schemas']
type JsonResponse<Operation extends keyof operations> = operations[Operation]['responses'] extends Record<200, { content: { 'application/json': infer Response } }>
  ? Response
  : never

export type Capabilities = Schemas['Capabilities']
export type WellKnown = JsonResponse<'getWellKnown'>
export type Marketplace = JsonResponse<'getInstance'>
export type RegistryRoot = Schemas['RegistryRoot']
export type RegistryPublisher = Schemas['Publisher']

// Public registry metadata only. The API deliberately omits credentials, policy
// internals, and refresh diagnostics; origins are already sanitized server-side.
export type Registry = JsonResponse<'getRegistry'>
export type RegistrySummary = Schemas['RegistrySummary']

export type RegistryOccurrence = Schemas['RegistryOccurrence']
export type ExactRegistryOccurrence = Schemas['ExactRegistryOccurrence']

export type ResourceListItem = Schemas['ResourceListItem']
export type InspectionManifest = Schemas['InspectionManifest']
export type InspectionFile = Schemas['InspectionFile']
export type AssessmentFinding = Schemas['AssessmentFinding']
export type ResourceAssessment = Schemas['ResourceAssessment']
export type ResourceOccurrence = Schemas['ResourceOccurrence']
export type SearchResult = JsonResponse<'searchResources'>
export type ResourceRequirement = Schemas['ResourceRequirement']
export type ResourceRelation = Schemas['ResourceRelation']

type GeneratedResourceRevision = JsonResponse<'getResource'>

// Findings are rendered defensively so clients can retain byte-range fields
// from schema revisions where source_range is richer than today's string.
export type ResourceRevision = Omit<GeneratedResourceRevision, 'assessment'> & {
  assessment: Omit<GeneratedResourceRevision['assessment'], 'findings'> & { findings?: unknown[] }
}

export interface SearchParams {
  q: string
  kind: ResourceKind | ''
  registry: string[]
  offset: number
  limit: number
}

export interface FetchOptions {
  query?: Record<string, string | number | string[]>
  responseType?: 'json' | 'text' | 'blob'
}

export type ApiFetcher = <T>(request: string, options?: FetchOptions) => Promise<T>
