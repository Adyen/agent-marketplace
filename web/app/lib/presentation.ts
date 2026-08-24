import type { ResourceKind, ResourceListItem } from '~/types/marketplace'
import { isSafeRouteToken, isSafeResourceRoute } from '~/lib/routes'
import { isSha256Digest } from '~/lib/validation'

export const KIND_LABELS: Record<ResourceKind, string> = {
  skill: 'Skill',
  command: 'Command',
  subagent: 'Subagent',
  hook: 'Hook',
  mcpserver: 'MCP server',
  collection: 'Collection',
}

export const KIND_ICONS: Record<ResourceKind, string> = {
  skill: 'i-lucide-sparkles',
  command: 'i-lucide-terminal',
  subagent: 'i-lucide-bot',
  hook: 'i-lucide-webhook',
  mcpserver: 'i-lucide-plug',
  collection: 'i-lucide-library',
}

export function kindLabel(kind: ResourceKind): string {
  return KIND_LABELS[kind]
}

export function kindIcon(kind: ResourceKind): string {
  return KIND_ICONS[kind]
}

export function sourceLabel(item: ResourceListItem): string {
  return item.source_slug || item.source_id
}

export function safeCanonicalUrl(value: string | undefined): string {
  if (!value || hasUnsafeURLCharacters(value) || /%(?:0[0-9a-f]|1[0-9a-f]|7f)/i.test(value)) return ''
  try {
    const url = new URL(value)
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || !url.hostname || url.search || url.hash) return ''
    return `${url.origin}${url.pathname.replace(/\/+$/, '')}`
  } catch {
    return ''
  }
}

export function resourceLatestLocator(item: ResourceListItem, browserBase: string): string {
  const canonical = safeCanonicalUrl(browserBase)
  const source = item.source_slug || item.source_id
  if (!canonical || !isSafeRouteToken(item.source_id) || (item.source_slug !== undefined && !isSafeRouteToken(item.source_slug)) || !isSafeResourceRoute(source, item.kind, item.name)) return ''
  return `${canonical}/s/${encodeURIComponent(source)}/${encodeURIComponent(item.kind)}/${encodeURIComponent(item.name)}`
}

export function resourceLocator(item: ResourceListItem, browserBase: string): string {
  const latest = resourceLatestLocator(item, browserBase)
  if (!latest || !isSha256Digest(item.content_digest)) return ''
  return `${latest}/revisions/${encodeURIComponent(item.content_digest)}`
}

export function installCommand(item: ResourceListItem, browserBase: string): string {
  const locator = resourceLocator(item, browserBase)
  return locator ? `agent-marketplace install "${locator}"` : ''
}

function hasUnsafeURLCharacters(value: string): boolean {
  return [...value].some(char => {
    const code = char.charCodeAt(0)
    return code <= 0x1f || code === 0x7f || ' \'"`$;&|<>()[]{}*!?#\\'.includes(char)
  })
}
