import type { LocationQuery, LocationQueryRaw } from 'vue-router'
import { isResourceKind } from '~/lib/api'
import type { SearchParams } from '~/types/marketplace'

export const DEFAULT_LIMIT = 25
export const MAX_LIMIT = 100

export function parseSearchQuery(query: LocationQuery): SearchParams {
  const q = queryValue(query.q)
  const kindValue = queryValue(query.kind)
  const limitValue = Number.parseInt(queryValue(query.limit), 10)
  const offsetValue = Number.parseInt(queryValue(query.offset), 10)

  return {
    q,
    kind: isResourceKind(kindValue) ? kindValue : '',
    registry: queryValues(query.registry),
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
  if (params.registry.length) query.registry = params.registry
  if (params.offset > 0) query.offset = String(params.offset)
  query.limit = String(params.limit)
  return query
}

function queryValue(value: LocationQuery[string] | undefined): string {
  return Array.isArray(value) ? String(value[0] ?? '') : String(value ?? '')
}

function queryValues(value: LocationQuery[string] | undefined): string[] {
  return (Array.isArray(value) ? value : [value]).filter(item => item !== undefined).map(String)
}
