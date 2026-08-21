import type { ResourceKind, ResourceListItem } from '~/types/marketplace'

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

export function resourceLocator(item: ResourceListItem, canonicalURL: string): string {
  const canonical = safeCanonicalUrl(canonicalURL)
  if (!canonical) return ''
  const source = encodeURIComponent(item.source_slug || item.source_id)
  const kind = encodeURIComponent(item.kind)
  const name = encodeURIComponent(item.name)
  const digest = encodeURIComponent(item.content_digest)
  return `${canonical}/s/${source}/${kind}/${name}/revisions/${digest}`
}

export function installCommand(item: ResourceListItem, canonicalURL: string): string {
  const locator = resourceLocator(item, canonicalURL)
  return `agent-marketplace install "${locator}"`
}

function hasUnsafeURLCharacters(value: string): boolean {
  return [...value].some(char => {
    const code = char.charCodeAt(0)
    return code <= 0x1f || code === 0x7f || ' \'"`$;&|<>()[]{}*!?#\\'.includes(char)
  })
}
