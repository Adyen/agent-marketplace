import type { Row } from './controller'
import { compareRows, type SearchMatch } from './search'
import { readStoredValue, writeStoredValue } from './storage'

export type GroupBy = 'status' | 'source' | 'kind'
export type ThenBy = 'none' | 'kind' | 'source'
export interface ViewSettings {
  groupBy: GroupBy
  thenBy: ThenBy
}

export interface Section {
  path: string
  title: string
  depth: 0 | 1
  count: number
  rows: Row[]
  children: Section[]
}

export interface StatusGroupMeta {
  id: string
  title: string
}

export const DEFAULT_VIEW: ViewSettings = { groupBy: 'status', thenBy: 'none' }
export const VIEW_STORAGE_KEY = 'am-gui-view'

const VIEW_STORAGE_VERSION = 1
const GROUP_BY_AXES: readonly GroupBy[] = ['status', 'source', 'kind']
const THEN_BY_AXES: readonly ThenBy[] = ['none', 'kind', 'source']
const ATTENTION_GROUPS = new Set(['pending', 'stale', 'broken', 'malformed'])
const KIND_ORDER = ['skill', 'command', 'subagent', 'hook', 'mcpserver', 'collection', 'rule']
const KIND_TITLES = new Map([
  ['skill', 'Skills'],
  ['command', 'Commands'],
  ['subagent', 'Subagents'],
  ['hook', 'Hooks'],
  ['mcpserver', 'MCP servers'],
  ['collection', 'Collections'],
  ['rule', 'Rules'],
])

interface Bucket {
  segment: string
  title: string
  rows: Row[]
}

export function thenByChoices(groupBy: GroupBy): ThenBy[] {
  return THEN_BY_AXES.filter((axis) => axis !== groupBy)
}

function isGroupBy(value: unknown): value is GroupBy {
  return GROUP_BY_AXES.includes(value as GroupBy)
}

function isThenBy(value: unknown): value is ThenBy {
  return THEN_BY_AXES.includes(value as ThenBy)
}

export function normalizeView(raw: unknown): ViewSettings {
  if (typeof raw !== 'object' || raw === null) {
    return { ...DEFAULT_VIEW }
  }
  const { groupBy, thenBy } = raw as { groupBy?: unknown; thenBy?: unknown }
  if (!isGroupBy(groupBy) || !isThenBy(thenBy) || thenBy === groupBy) {
    return { ...DEFAULT_VIEW }
  }
  return { groupBy, thenBy }
}

export function readViewSettings(): ViewSettings {
  const stored = readStoredValue(VIEW_STORAGE_KEY)
  if (stored == null) {
    return { ...DEFAULT_VIEW }
  }
  try {
    const parsed = JSON.parse(stored) as { version?: unknown } | null
    return parsed?.version === VIEW_STORAGE_VERSION ? normalizeView(parsed) : { ...DEFAULT_VIEW }
  } catch {
    return { ...DEFAULT_VIEW }
  }
}

export function writeViewSettings(settings: ViewSettings) {
  writeStoredValue(
    VIEW_STORAGE_KEY,
    JSON.stringify({ version: VIEW_STORAGE_VERSION, groupBy: settings.groupBy, thenBy: settings.thenBy }),
  )
}

function compareText(left: string, right: string): number {
  if (left < right) {
    return -1
  }
  return left > right ? 1 : 0
}

function kindRank(kind: string): number {
  const index = KIND_ORDER.indexOf(kind)
  return index === -1 ? KIND_ORDER.length : index
}

function groupRowsBy(rows: Row[], keyOf: (row: Row) => string): Map<string, Row[]> {
  const groups = new Map<string, Row[]>()
  for (const row of rows) {
    const key = keyOf(row)
    const members = groups.get(key)
    if (members == null) {
      groups.set(key, [row])
    } else {
      members.push(row)
    }
  }
  return groups
}

function bucketize(rows: Row[], axis: GroupBy | ThenBy, statusGroups: readonly StatusGroupMeta[]): Bucket[] {
  switch (axis) {
    case 'status': {
      const byGroup = groupRowsBy(rows, (row) => row.group)
      return statusGroups.flatMap((group) => {
        const members = byGroup.get(group.id)
        return members == null ? [] : [{ segment: group.id, title: group.title, rows: members }]
      })
    }
    case 'kind':
      return [...groupRowsBy(rows, (row) => row.kind).entries()]
        .sort(([left], [right]) => kindRank(left) - kindRank(right) || compareText(left, right))
        .map(([kind, members]) => ({
          segment: `kind:${encodeURIComponent(kind)}`,
          title: KIND_TITLES.get(kind) ?? kind,
          rows: members,
        }))
    case 'source':
      return [...groupRowsBy(rows, (row) => row.source.id).entries()]
        .map(([id, members]) => ({
          id,
          title: members.map((row) => row.source.label).reduce((least, label) => (label < least ? label : least)),
          rows: members,
        }))
        .sort((left, right) => compareText(left.title, right.title) || compareText(left.id, right.id))
        .map(({ id, title, rows: members }) => ({ segment: `src:${encodeURIComponent(id)}`, title, rows: members }))
  }
  return []
}

function joinPath(parent: string, segment: string): string {
  return `${parent}/${segment}`
}

export function buildSections(
  matches: SearchMatch[],
  statusGroups: readonly StatusGroupMeta[],
  settings: ViewSettings,
  filtered: boolean,
): Section[] {
  const rows = matches.map((match) => match.row)
  const pinAttention = settings.groupBy !== 'status'
  const attentionRows = pinAttention ? rows.filter((row) => ATTENTION_GROUPS.has(row.group)) : []
  const remaining = pinAttention ? rows.filter((row) => !ATTENTION_GROUPS.has(row.group)) : rows
  const statusRank = new Map(statusGroups.map((group, index) => [group.id, index]))
  const rankOf = (row: Row) => statusRank.get(row.group) ?? statusGroups.length

  const sections: Section[] = []
  if (attentionRows.length > 0) {
    const sorted = [...attentionRows].sort((left, right) => rankOf(left) - rankOf(right) || compareRows(left, right))
    sections.push({ path: 'attention', title: 'Needs attention', depth: 0, count: sorted.length, rows: sorted, children: [] })
  }

  const leafRows = (members: Row[]) => (filtered ? members : [...members].sort(compareRows))
  for (const bucket of bucketize(remaining, settings.groupBy, statusGroups)) {
    if (settings.thenBy === 'none') {
      sections.push({ path: bucket.segment, title: bucket.title, depth: 0, count: bucket.rows.length, rows: leafRows(bucket.rows), children: [] })
      continue
    }
    const children = bucketize(bucket.rows, settings.thenBy, statusGroups).map((child): Section => ({
      path: joinPath(bucket.segment, child.segment),
      title: child.title,
      depth: 1,
      count: child.rows.length,
      rows: leafRows(child.rows),
      children: [],
    }))
    sections.push({ path: bucket.segment, title: bucket.title, depth: 0, count: bucket.rows.length, rows: [], children })
  }
  return sections
}

export function sectionPathsContaining(sections: Section[], rowId: string): string[] {
  for (const section of sections) {
    if (section.rows.some((row) => row.id === rowId)) {
      return [section.path]
    }
    const inner = sectionPathsContaining(section.children, rowId)
    if (inner.length > 0) {
      return [section.path, ...inner]
    }
  }
  return []
}
