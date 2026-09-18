const API_VERSION_PATH = "/api/v1";
const WELL_KNOWN_PATH = "/.well-known/agent-marketplace.json";

export function normalizeApiBase(rawBase: string | undefined): string {
  const value = (rawBase ?? "/").trim();
  if (!value || value === "/") return "";
  if (hasUnsafeURLCharacters(value)) return "";

  if (/^https?:\/\//i.test(value)) {
    try {
      const url = new URL(value);
      if (
        url.username ||
        url.password ||
        url.search ||
        url.hash ||
        !url.hostname
      )
        return "";
      const pathname = trimTrailingSlashes(url.pathname);
      return `${url.origin}${pathname}`;
    } catch {
      return "";
    }
  }

  if (value.startsWith("//") || (!value.startsWith("/") && value.includes(":")))
    return "";
  const path = value.startsWith("/") ? value : `/${value}`;
  return trimTrailingSlashes(path);
}

export function isAbsoluteApiBase(value: string | undefined): boolean {
  return /^https?:\/\//i.test((value ?? "").trim());
}

export function apiRoot(apiBase: string | undefined): string {
  const base = normalizeApiBase(apiBase);
  return base.endsWith(API_VERSION_PATH) ? base : `${base}${API_VERSION_PATH}`;
}

export function apiPath(apiBase: string | undefined, path: string): string {
  return joinURL(apiRoot(apiBase), path);
}

export function wellKnownPath(apiBase: string | undefined): string {
  const base = normalizeApiBase(apiBase);
  const root = base.endsWith(API_VERSION_PATH)
    ? base.slice(0, -API_VERSION_PATH.length)
    : base;
  return joinURL(root, WELL_KNOWN_PATH);
}

export function devApiProxyTargets(
  value: string | undefined,
): { api: string; wellKnown: string } | null {
  const base = normalizeApiBase(value);
  if (!isAbsoluteApiBase(base)) return null;
  return { api: apiRoot(base), wellKnown: wellKnownPath(base) };
}

function hasUnsafeURLCharacters(value: string): boolean {
  return [...value].some((char) => {
    const code = char.charCodeAt(0);
    return (
      code <= 0x1f || code === 0x7f || " '\"`$;&|<>()[]{}*!?#\\".includes(char)
    );
  });
}

function trimTrailingSlashes(value: string): string {
  return value.replace(/\/+$/, "");
}

function joinURL(base: string, path: string): string {
  return `${base}${path.startsWith("/") ? path : `/${path}`}` || "/";
}
