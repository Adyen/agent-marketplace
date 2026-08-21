import type { LocationQuery, LocationQueryRaw } from 'vue-router'
import { isResourceKind } from '~/lib/api'
import { ALL_RESOURCE_KINDS } from '~/types/marketplace'
import type { ResourceKindFilter, SearchParams } from '~/types/marketplace'

export const DEFAULT_LIMIT = 12
export const MAX_LIMIT = 100

export function parseSearchQuery(query: LocationQuery): SearchParams {
  const q = queryValue(query.q)
  const kindValue = queryValue(query.kind)
  const limitValue = Number.parseInt(queryValue(query.limit), 10)
  const offsetValue = Number.parseInt(queryValue(query.offset), 10)

  return {
    q,
    kind: isResourceKind(kindValue) ? kindValue : '',
    source: queryValue(query.source),
    offset: Number.isFinite(offsetValue) && offsetValue > 0 ? offsetValue : 0,
    limit: Number.isFinite(limitValue)
      ? Math.min(Math.max(limitValue, 1), MAX_LIMIT)
      : DEFAULT_LIMIT,
  }
}

export function toLocationQuery(params: SearchParams): LocationQueryRaw {
  const query: LocationQueryRaw = {}
  if (params.q) query.q = params.q
  if (params.kind) query.kind = params.kind
  if (params.source) query.source = params.source
  if (params.offset > 0) query.offset = String(params.offset)
  query.limit = String(params.limit)
  return query
}

export function toResourceKindFilter(kind: SearchParams['kind']): ResourceKindFilter {
  return kind || ALL_RESOURCE_KINDS
}

export function fromResourceKindFilter(kind: ResourceKindFilter): SearchParams['kind'] {
  return kind === ALL_RESOURCE_KINDS ? '' : kind
}

function queryValue(value: LocationQuery[string] | undefined): string {
  return Array.isArray(value) ? String(value[0] ?? '') : String(value ?? '')
}
