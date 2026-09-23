import type { LocationQuery, LocationQueryRaw } from "vue-router";
import { isResourceKind } from "~/lib/api";
import type { SearchParams } from "~/types/marketplace";

export const DEFAULT_LIMIT = 25;

export function parseSearchQuery(query: LocationQuery): SearchParams {
  const q = queryValue(query.q);
  const kindValue = queryValue(query.kind);

  return {
    q,
    kind: isResourceKind(kindValue) ? kindValue : "",
    registry: queryValues(query.registry),
    offset: 0,
    limit: DEFAULT_LIMIT,
  };
}

export function toLocationQuery(params: SearchParams): LocationQueryRaw {
  const query: LocationQueryRaw = {};
  if (params.q) query.q = params.q;
  if (params.kind) query.kind = params.kind;
  if (params.registry.length) query.registry = params.registry;
  return query;
}

function queryValue(value: LocationQuery[string] | undefined): string {
  return Array.isArray(value) ? String(value[0] ?? "") : String(value ?? "");
}

function queryValues(value: LocationQuery[string] | undefined): string[] {
  return (Array.isArray(value) ? value : [value])
    .filter((item) => item !== undefined)
    .map(String);
}
