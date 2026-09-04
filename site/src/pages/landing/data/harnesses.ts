/**
 * This build has no generator for harnesses.json. It remains a checked-in
 * fixture until documentation generation adds a drift check.
 */
import raw from './harnesses.json'

export type Status = 'native' | 'shared' | 'as-skill' | 'none'

interface Target {
  id: string
  label: string
  repoRoots: string[]
  machineRoots: string[]
  capabilities: Record<string, boolean>
}

interface Kind {
  id: string
  label: string
  selectable: boolean
  experimental: boolean
  note?: string
}

interface Cell {
  status: Status
  paths?: string[]
  codes?: string[]
}

interface HarnessFixture {
  targets: Target[]
  kinds: Kind[]
  matrix: Record<string, Record<string, Cell>>
}

const STATUSES = new Set<Status>(['native', 'shared', 'as-skill', 'none'])

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value)

const isStringArray = (value: unknown): value is string[] =>
  Array.isArray(value) && value.every((item) => typeof item === 'string')

const isTarget = (value: unknown): value is Target =>
  isRecord(value) &&
  typeof value.id === 'string' &&
  typeof value.label === 'string' &&
  isStringArray(value.repoRoots) &&
  isStringArray(value.machineRoots) &&
  isRecord(value.capabilities) &&
  Object.values(value.capabilities).every((capability) => typeof capability === 'boolean')

const isKind = (value: unknown): value is Kind =>
  isRecord(value) &&
  typeof value.id === 'string' &&
  typeof value.label === 'string' &&
  typeof value.selectable === 'boolean' &&
  typeof value.experimental === 'boolean' &&
  (value.note === undefined || typeof value.note === 'string')

const isCell = (value: unknown): value is Cell =>
  isRecord(value) &&
  typeof value.status === 'string' &&
  STATUSES.has(value.status as Status) &&
  (value.paths === undefined || isStringArray(value.paths)) &&
  (value.codes === undefined || isStringArray(value.codes))

function malformed(reason: string): never {
  throw new Error(`Malformed harnesses.json fixture: ${reason}`)
}

function loadFixture(value: unknown): HarnessFixture {
  if (
    !isRecord(value) ||
    !Array.isArray(value.targets) ||
    !Array.isArray(value.kinds) ||
    !isRecord(value.matrix)
  ) {
    return malformed('expected targets, kinds, and matrix')
  }
  if (!value.targets.every(isTarget)) return malformed('invalid target')
  if (!value.kinds.every(isKind)) return malformed('invalid kind')

  const targetIds = new Set(value.targets.map((target) => target.id))
  const kindIds = new Set(value.kinds.map((kind) => kind.id))
  if (targetIds.size !== value.targets.length) return malformed('duplicate target id')
  if (kindIds.size !== value.kinds.length) return malformed('duplicate kind id')

  for (const [kindId, kindMatrix] of Object.entries(value.matrix)) {
    if (!kindIds.has(kindId)) return malformed(`unknown kind "${kindId}"`)
    if (!isRecord(kindMatrix)) return malformed(`invalid matrix for kind "${kindId}"`)
    for (const [targetId, cell] of Object.entries(kindMatrix)) {
      if (!targetIds.has(targetId)) return malformed(`unknown target "${targetId}"`)
      if (!isCell(cell)) return malformed(`invalid cell "${kindId}.${targetId}"`)
    }
  }

  for (const kindId of kindIds) {
    const kindMatrix = value.matrix[kindId]
    if (!isRecord(kindMatrix)) return malformed(`missing matrix for kind "${kindId}"`)
    for (const targetId of targetIds) {
      if (!isCell(kindMatrix[targetId])) return malformed(`missing cell "${kindId}.${targetId}"`)
    }
  }

  return {
    targets: value.targets,
    kinds: value.kinds,
    matrix: value.matrix as Record<string, Record<string, Cell>>
  }
}

const fixture = loadFixture(raw)

export const targets = fixture.targets
export const kinds = fixture.kinds
const matrix = fixture.matrix

const cellFor = (kindId: string, targetId: string): Cell => {
  const kindMatrix = matrix[kindId]
  if (!kindMatrix) return { status: 'none' }
  return kindMatrix[targetId] ?? { status: 'none' }
}

const byId = new Map(targets.map((target) => [target.id, target]))

/**
 * Emitters record artifact paths relative to whichever root they own, so some
 * already carry the harness directory and some do not (Gemini writes
 * `.gemini/agents/x.md` but `commands/x.toml`). Prefix only when it is missing,
 * so every path reads from the repository root.
 */
function fromRepoRoot(path: string, repoRoots: string[]) {
  // Windsurf's root is the repository itself, recorded as "./ (repository root)".
  const dirs = repoRoots.filter((root) => root.startsWith('.') && !root.includes(' '))
  if (!dirs.length) return path
  if (dirs.some((dir) => path === dir || path.startsWith(`${dir}/`))) return path
  return `${dirs[0]}/${path}`
}

/**
 * The probe compiles a resource literally named "example". Swapping in the
 * names the page already uses keeps one vocabulary across the whole story; only
 * that placeholder segment is replaced, never any real path text.
 */
const rename = (path: string, name?: string) =>
  name ? path.replace(/(^|\/)example(\/|\.|$)/, `$1${name}$2`) : path

interface OutputRow {
  kindId: string
  kindLabel: string
  status: Status
  lead?: string
  extra: string[]
  codes: string[]
}

export function outputRows(
  targetId: string,
  kindIds: string[],
  exampleNames: Record<string, string> = {}
): OutputRow[] {
  const target = byId.get(targetId)
  if (!target) return []

  return kindIds.flatMap((kindId) => {
    const kind = kinds.find((candidate) => candidate.id === kindId)
    if (!kind) return []

    const cell = cellFor(kindId, targetId)
    const name = exampleNames[kindId]
    const paths = (cell.paths ?? []).map((path) =>
      fromRepoRoot(rename(path, name), target.repoRoots)
    )

    // Prefer the resource-specific path when compilation emits supporting files.
    const lead = (name && paths.find((path) => path.includes(name))) || paths[0]

    return [
      {
        kindId,
        kindLabel: kind.label,
        status: cell.status,
        lead,
        extra: paths.filter((path) => path !== lead),
        codes: cell.codes ?? []
      }
    ]
  })
}

export function statusesUsed(kindIds: string[]): Status[] {
  const seen = new Set<Status>()
  for (const kindId of kindIds) {
    for (const target of targets) {
      seen.add(cellFor(kindId, target.id).status)
    }
  }
  return [...seen]
}
