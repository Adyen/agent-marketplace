// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  buildSections,
  DEFAULT_VIEW,
  normalizeView,
  readViewSettings,
  sectionPathsContaining,
  thenByChoices,
  VIEW_STORAGE_KEY,
  writeViewSettings,
  type GroupBy,
  type Section,
  type ViewSettings,
} from './grouping'
import { row, snapshot } from './fixtures'
import { rankRows } from './search'
import type { Row } from './controller'

const STATUS_GROUPS = snapshot().groups
const LOCAL = { id: 'local', label: '(local)' }
const TEAM_MAIN = { id: 'https://example.com/team.git\x01main\x01', label: 'team' }
const TEAM_RELEASE = { id: 'https://example.com/team.git\x01release\x01', label: 'team' }
const TEAM_PLUGINS = { id: 'https://example.com/team.git\x01main\x01plugins/a', label: 'team / plugins/a' }

function resources(): Row[] {
  return [
    row('alpha', { kind: 'skill', group: 'enabled', status: 'enabled', source: TEAM_MAIN }),
    row('beta', { kind: 'command', group: 'pending', status: 'enabled (pending sync)', source: TEAM_RELEASE }),
    row('gamma', { kind: 'mcpserver', group: 'broken', status: 'broken', source: LOCAL }),
    row('delta', { kind: 'zed-kind', group: 'available', checked: false, status: 'available', source: TEAM_PLUGINS }),
    row('epsilon', { kind: 'skill', group: 'malformed', status: 'malformed: invalid', source: LOCAL }),
    row('zeta', { kind: 'rule', group: 'required', required: true, status: 'required', source: TEAM_MAIN }),
    row('eta', { kind: 'hook', group: 'stale', status: 'available, stale', source: LOCAL }),
  ]
}

function allCombinations(): ViewSettings[] {
  const groupBys: GroupBy[] = ['status', 'source', 'kind']
  return groupBys.flatMap((groupBy) => thenByChoices(groupBy).map((thenBy) => ({ groupBy, thenBy })))
}

function leafRows(sections: Section[]): Row[] {
  return sections.flatMap((section) => (section.children.length > 0 ? leafRows(section.children) : section.rows))
}

function flatten(sections: Section[]): Section[] {
  return sections.flatMap((section) => [section, ...flatten(section.children)])
}

function names(section: Section): string[] {
  return section.rows.map((candidate) => candidate.name)
}

beforeEach(() => {
  window.localStorage.clear()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('view grouping choices', () => {
  it('offers None, Kind, and Source as Then by, never the primary axis', () => {
    expect(thenByChoices('status')).toEqual(['none', 'kind', 'source'])
    expect(thenByChoices('kind')).toEqual(['none', 'source'])
    expect(thenByChoices('source')).toEqual(['none', 'kind'])
  })

  it('normalizes missing, non-object, invalid, and identical axes to the default view', () => {
    expect(normalizeView(undefined)).toEqual(DEFAULT_VIEW)
    expect(normalizeView('kind')).toEqual(DEFAULT_VIEW)
    expect(normalizeView({ groupBy: 'status' })).toEqual(DEFAULT_VIEW)
    expect(normalizeView({ groupBy: 'none', thenBy: 'none' })).toEqual(DEFAULT_VIEW)
    expect(normalizeView({ groupBy: 'kind', thenBy: 'kind' })).toEqual(DEFAULT_VIEW)
    expect(normalizeView({ groupBy: 'source', thenBy: 'status' })).toEqual(DEFAULT_VIEW)
    expect(normalizeView({ groupBy: 'kind', thenBy: 'source' })).toEqual({ groupBy: 'kind', thenBy: 'source' })
  })

  // spec: gui-shell/grouping-choice-persists-locally-without-workspace-writes
  it('restores a stored choice and falls back to Status and None for unusable storage', () => {
    writeViewSettings({ groupBy: 'source', thenBy: 'kind' })
    expect(window.localStorage.getItem(VIEW_STORAGE_KEY)).toBe('{"version":1,"groupBy":"source","thenBy":"kind"}')
    expect(readViewSettings()).toEqual({ groupBy: 'source', thenBy: 'kind' })

    window.localStorage.setItem(VIEW_STORAGE_KEY, '{not json')
    expect(readViewSettings()).toEqual(DEFAULT_VIEW)

    window.localStorage.setItem(VIEW_STORAGE_KEY, '{"version":2,"groupBy":"kind","thenBy":"none"}')
    expect(readViewSettings()).toEqual(DEFAULT_VIEW)

    window.localStorage.setItem(VIEW_STORAGE_KEY, '{"version":1,"groupBy":"kind","thenBy":"kind"}')
    expect(readViewSettings()).toEqual(DEFAULT_VIEW)

    window.localStorage.setItem(VIEW_STORAGE_KEY, 'null')
    expect(readViewSettings()).toEqual(DEFAULT_VIEW)

    window.localStorage.clear()
    expect(readViewSettings()).toEqual(DEFAULT_VIEW)
  })

  // spec: gui-shell/grouping-choice-persists-locally-without-workspace-writes
  it('uses the default view and swallows writes when browser storage throws', () => {
    vi.spyOn(window.localStorage, 'getItem').mockImplementation(() => {
      throw new Error('storage disabled')
    })
    vi.spyOn(window.localStorage, 'setItem').mockImplementation(() => {
      throw new Error('storage disabled')
    })
    expect(readViewSettings()).toEqual(DEFAULT_VIEW)
    expect(() => writeViewSettings({ groupBy: 'kind', thenBy: 'none' })).not.toThrow()
  })
})

describe('buildSections', () => {
  // spec: gui-shell/every-valid-grouping-combination-retains-distinct-resources
  it('shows every resource once under every valid grouping combination', () => {
    const rows = resources()
    for (const settings of allCombinations()) {
      const sections = buildSections(rankRows(rows, ''), STATUS_GROUPS, settings, false)
      const seen = leafRows(sections).map((candidate) => candidate.id).sort()
      expect(seen, JSON.stringify(settings)).toEqual(rows.map((candidate) => candidate.id).sort())
      for (const section of flatten(sections)) {
        expect(section.count, section.path).toBeGreaterThan(0)
      }
    }
  })

  // spec: gui-shell/every-valid-grouping-combination-retains-distinct-resources
  it('orders status sections from the snapshot and kind sections by known kind then name', () => {
    const rows = resources()
    const status = buildSections(rankRows(rows, ''), STATUS_GROUPS, { groupBy: 'status', thenBy: 'none' }, false)
    expect(status.map((section) => section.path)).toEqual(['required', 'enabled', 'pending', 'stale', 'broken', 'available', 'malformed'])

    const kind = buildSections(rankRows(rows, ''), STATUS_GROUPS, { groupBy: 'kind', thenBy: 'none' }, false)
    expect(kind.map((section) => section.title)).toEqual(['Needs attention', 'Skills', 'Rules', 'zed-kind'])
    expect(kind.map((section) => section.path)).toEqual(['attention', 'kind:skill', 'kind:rule', 'kind:zed-kind'])
  })

  // spec: gui-shell/every-valid-grouping-combination-retains-distinct-resources
  it('pins attention rows in status-then-name order outside Status grouping', () => {
    const rows = [
      row('zulu', { kind: 'skill', group: 'broken', status: 'broken', source: LOCAL }),
      row('alpha', { kind: 'skill', group: 'pending', status: 'enabled (pending sync)', source: LOCAL }),
      row('bravo', { kind: 'skill', group: 'pending', status: 'enabled (pending sync)', source: LOCAL }),
      row('charlie', { kind: 'skill', group: 'required', required: true, status: 'required', source: LOCAL }),
    ]
    const sections = buildSections(rankRows(rows, ''), STATUS_GROUPS, { groupBy: 'kind', thenBy: 'none' }, false)
    expect(sections[0]).toMatchObject({ path: 'attention', title: 'Needs attention', depth: 0, count: 3, children: [] })
    expect(names(sections[0])).toEqual(['alpha', 'bravo', 'zulu'])
    expect(leafRows(sections).map((candidate) => candidate.name).sort()).toEqual(['alpha', 'bravo', 'charlie', 'zulu'])
  })

  // spec: gui-shell/every-valid-grouping-combination-retains-distinct-resources
  it('keeps sources with equal labels distinct and orders them by label then identity', () => {
    const rows = [
      row('one', { kind: 'skill', group: 'enabled', source: TEAM_RELEASE }),
      row('two', { kind: 'skill', group: 'enabled', source: TEAM_MAIN }),
    ]
    const forward = buildSections(rankRows(rows, ''), STATUS_GROUPS, { groupBy: 'source', thenBy: 'none' }, false)
    const backward = buildSections(rankRows([...rows].reverse(), ''), STATUS_GROUPS, { groupBy: 'source', thenBy: 'none' }, false)
    expect(forward.map((section) => section.title)).toEqual(['team', 'team'])
    expect(new Set(forward.map((section) => section.path)).size).toBe(2)
    expect(forward.map((section) => section.path)).toEqual(backward.map((section) => section.path))
    expect(forward.map((section) => names(section))).toEqual([['two'], ['one']])
  })

  // spec: gui-shell/every-valid-grouping-combination-retains-distinct-resources
  it('nests the secondary axis one level below its primary section', () => {
    const sections = buildSections(rankRows(resources(), ''), STATUS_GROUPS, { groupBy: 'kind', thenBy: 'source' }, false)
    const skills = sections.find((section) => section.title === 'Skills')
    expect(skills?.rows).toEqual([])
    expect(skills?.count).toBe(1)
    expect(skills?.children.map((child) => child.depth)).toEqual([1])
    expect(skills?.children.map((child) => child.path)).toEqual([`kind:skill/src:${encodeURIComponent(TEAM_MAIN.id)}`])
    expect(skills?.children.map((child) => names(child))).toEqual([['alpha']])
  })

  // spec: gui-shell/every-valid-grouping-combination-retains-distinct-resources
  it('omits empty sections under every grouping', () => {
    const rows = [row('only', { kind: 'skill', group: 'enabled', source: LOCAL })]
    const sections = buildSections(rankRows(rows, ''), STATUS_GROUPS, { groupBy: 'kind', thenBy: 'none' }, false)
    expect(sections.map((section) => section.path)).toEqual(['kind:skill'])
    expect(buildSections([], STATUS_GROUPS, { groupBy: 'status', thenBy: 'none' }, false)).toEqual([])
  })

  it('sorts unfiltered leaves by name and keeps ranked order while filtered', () => {
    const rows = [
      row('bravo', { group: 'enabled', source: LOCAL }),
      row('alpha', { group: 'enabled', source: LOCAL }),
    ]
    const settings: ViewSettings = { groupBy: 'status', thenBy: 'none' }
    expect(names(buildSections(rankRows(rows, ''), STATUS_GROUPS, settings, false)[0])).toEqual(['alpha', 'bravo'])
    expect(names(buildSections(rankRows(rows, ''), STATUS_GROUPS, settings, true)[0])).toEqual(['bravo', 'alpha'])
  })
})

describe('section paths', () => {
  it('finds the enclosing section chain for a row', () => {
    const rows = resources()
    const sections = buildSections(rankRows(rows, ''), STATUS_GROUPS, { groupBy: 'kind', thenBy: 'source' }, false)
    const chain = sectionPathsContaining(sections, rows[0].id)
    expect(chain).toHaveLength(2)
    expect(chain[0]).toBe('kind:skill')
    expect(sectionPathsContaining(sections, 'missing')).toEqual([])
  })
})
