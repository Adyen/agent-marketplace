import type { SourceDescription } from './api/client'
import { sourceLabel } from './source-label'

export type SourceLayer = 'user' | 'organization'

export type LayeredSource = { source: SourceDescription; layer: SourceLayer }

export type SourceGroupEntry =
  | { type: 'source'; key: string; layer: SourceLayer; source: SourceDescription }
  | { type: 'group'; key: string; layer: SourceLayer; kind: SourceDescription['kind']; location: string; sources: SourceDescription[] }

function matchesSource(source: SourceDescription, query: string): boolean {
  return [source.name, sourceLabel(source), source.location, source.path].some((value) =>
    value.toLowerCase().includes(query.toLowerCase()),
  )
}

export function groupSources(sources: readonly LayeredSource[], query = ''): SourceGroupEntry[] {
  const grouped = new Map<string, LayeredSource[]>()
  for (const item of sources) {
    const key = JSON.stringify([item.layer, item.source.kind, item.source.location])
    const group = grouped.get(key) ?? []
    group.push(item)
    grouped.set(key, group)
  }

  const entries: SourceGroupEntry[] = []
  for (const [key, group] of grouped) {
    const matches = query === '' ? group : group.filter(({ source }) => matchesSource(source, query))
    if (matches.length === 0) {
      continue
    }
    const first = group[0]!
    if (group.length === 1) {
      entries.push({ type: 'source', key, layer: first.layer, source: first.source })
    } else {
      entries.push({
        type: 'group',
        key,
        layer: first.layer,
        kind: first.source.kind,
        location: first.source.location,
        sources: matches.map(({ source }) => source),
      })
    }
  }
  return entries
}

export type SourceGroupRow =
  | { type: 'group'; key: string; entry: Extract<SourceGroupEntry, { type: 'group' }> }
  | { type: 'source'; key: string; source: SourceDescription; layer: SourceLayer; groupKey: string | null }

export function flattenSourceGroups(entries: readonly SourceGroupEntry[], folded: ReadonlySet<string>): SourceGroupRow[] {
  return entries.flatMap<SourceGroupRow>((entry) =>
    entry.type === 'source'
      ? [{ type: 'source', key: entry.key, source: entry.source, layer: entry.layer, groupKey: null }]
      : [
          { type: 'group' as const, key: entry.key, entry },
          ...(folded.has(entry.key)
            ? []
            : entry.sources.map((source) => ({
                type: 'source' as const,
                key: source.id,
                source,
                layer: entry.layer,
                groupKey: entry.key,
              }))),
        ],
  )
}
