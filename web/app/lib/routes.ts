import type { ResourceKind } from "~/types/marketplace";
import { isResourceKind } from "~/lib/api";

// Route values are decoded by vue-router/Nuxt before they reach page setup. Keep
// this grammar deliberately narrower than a general URL segment: a slash (or an
// encoded slash) must never become part of a human-facing CLI locator.
const ROUTE_TOKEN_PATTERN =
  /^(?=.{1,256}$)[A-Za-z0-9](?:[A-Za-z0-9._:@+~ -]*[A-Za-z0-9._:@+~-])?$/;

export function isSafeRouteToken(value: unknown): value is string {
  return (
    typeof value === "string" &&
    ROUTE_TOKEN_PATTERN.test(value) &&
    !value.includes("%")
  );
}

export function routeParam(
  value: string | string[] | undefined,
  label: string,
): string {
  const candidate = Array.isArray(value)
    ? value.length === 1
      ? value[0]
      : undefined
    : value;
  if (!isSafeRouteToken(candidate)) {
    throw createError({ statusCode: 404, statusMessage: `Invalid ${label}` });
  }
  return candidate;
}

export function registryAsyncDataKey(registry: string): string {
  return JSON.stringify(["marketplace-registry", registry]);
}

export function registryHistoryAsyncDataKey(registry: string): string {
  return JSON.stringify(["marketplace-registry-history", registry]);
}

export function resourceAsyncDataKey(
  registry: string,
  kind: ResourceKind,
  name: string,
): string {
  return JSON.stringify(["marketplace-resource", registry, kind, name]);
}

export function revisionAsyncDataKey(
  registry: string,
  kind: ResourceKind,
  name: string,
  revision: string,
): string {
  return JSON.stringify([
    "marketplace-resource-revision",
    registry,
    kind,
    name,
    revision,
  ]);
}

export function resourceInspectionAsyncDataKey(
  registry: string,
  kind: ResourceKind,
  name: string,
  revision: string,
): string {
  return JSON.stringify([
    "marketplace-resource-inspection",
    registry,
    kind,
    name,
    revision,
  ]);
}

export function resourceInspectionContentAsyncDataKey(
  registry: string,
  kind: ResourceKind,
  name: string,
  revision: string,
): string {
  return JSON.stringify([
    "marketplace-resource-inspection-content",
    registry,
    kind,
    name,
    revision,
  ]);
}

export function isSafeResourceRoute(
  registry: string,
  kind: string,
  name: string,
  revision?: string,
): boolean {
  return (
    isSafeRouteToken(registry) &&
    isResourceKind(kind) &&
    isSafeRouteToken(name) &&
    (revision === undefined || isSafeRouteToken(revision))
  );
}
