import { describe, expect, it } from 'vitest'
import type { SourceDescription } from './api/client'
import { shortSourceLocation, sourceDisplayLabel, sourceLabel } from './source-label'

function source(kind: SourceDescription['kind'], location: string, name = ''): SourceDescription {
  return { id: 'id', name, kind, location, path: '', ref: '', sourceHarness: '' } as SourceDescription
}

describe('sourceDisplayLabel', () => {
  it('uses the configured name for a named local source and the row label otherwise', () => {
    expect(sourceDisplayLabel(source('git', 'https://git.example.test/team.git', 'Team'), 'ignored')).toBe('Team')
    expect(sourceDisplayLabel(source('directory', '/code/skills'), 'row-label')).toBe('row-label')
    expect(sourceDisplayLabel(source('directory', '/code/skills'))).toBe('code/skills')
  })

  it('keeps the endpoint label for a Marketplace source', () => {
    expect(sourceDisplayLabel(source('marketplace', 'https://marketplace.example.com/api', 'Hub'))).toBe('marketplace.example.com/api')
  })
})

describe('shortSourceLocation', () => {
  it('keeps the last two path segments of a directory or Git location', () => {
    expect(shortSourceLocation('/Users/alexandergr/code/ai/resources/skills')).toBe('resources/skills')
    expect(shortSourceLocation('https://git.example.test/org/team.git')).toBe('org/team')
    expect(shortSourceLocation('git@git.example.test:org/team.git')).toBe('org/team')
  })

  it('returns a single-segment or unparseable location unchanged', () => {
    expect(shortSourceLocation('/skills')).toBe('skills')
    expect(shortSourceLocation('https://')).toBe('https://')
  })
})

describe('sourceLabel', () => {
  it('shows the last two segments of a directory', () => {
    expect(sourceLabel(source('directory', '/Users/alexandergr/code/ai/resources/skills'))).toBe('resources/skills')
    expect(sourceLabel(source('directory', '/Users/alexandergr/code/ai/resources/skills/'))).toBe('resources/skills')
    expect(sourceLabel(source('directory', '~/regs/team'))).toBe('regs/team')
  })

  it('reads file URLs and keeps a single segment as is', () => {
    expect(sourceLabel(source('directory', 'file:///opt/org%20regs/shared'))).toBe('org regs/shared')
    expect(sourceLabel(source('directory', '/regs'))).toBe('regs')
  })

  // spec: gui-shell/marketplace-source-shows-its-endpoint-regardless-of-name
  it('labels a Marketplace source by its endpoint whatever its configured name', () => {
    expect(sourceLabel(source('marketplace', 'https://tools.example.com/acme', 'team-tools'))).toBe('tools.example.com/acme')
    expect(sourceLabel(source('marketplace', 'https://agent-marketplace.is.example.com'))).toBe('agent-marketplace.is.example.com')
    expect(sourceLabel(source('marketplace', 'https://agent-marketplace.is.example.com', 'marketplace'))).toBe('agent-marketplace.is.example.com')
    expect(sourceLabel(source('marketplace', 'https://mp.example.com:8443/acme/', 'marketplace-2'))).toBe('mp.example.com:8443/acme')
    expect(sourceLabel(source('marketplace', 'https://marketplace.example/services/catalog', 'marketplace'))).toBe('marketplace.example/catalog')
    expect(sourceLabel(source('marketplace', 'http://localhost:8443/api'))).toBe('localhost:8443/api')
  })

  // spec: gui-shell/named-local-source-shows-its-configured-name
  it('labels a named directory or Git source by its configured name', () => {
    expect(sourceLabel(source('directory', '/Users/alexandergr/code/ai/resources/skills', 'team-skills'))).toBe('team-skills')
    expect(sourceLabel(source('git', 'https://github.com/org/repo.git', 'shared-tools'))).toBe('shared-tools')
  })

  // spec: gui-shell/unnamed-local-source-keeps-its-path-label
  it('falls back to the path label for an unnamed directory or Git source', () => {
    expect(sourceLabel(source('directory', '/Users/alexandergr/code/ai/resources/skills', ''))).toBe('resources/skills')
    expect(sourceLabel(source('git', 'https://github.com/org/repo.git', ''))).toBe('org/repo')
  })

  it('keeps an IPv4 host whole', () => {
    expect(sourceLabel(source('marketplace', 'http://127.0.0.1:9000'))).toBe('127.0.0.1:9000')
  })

  it('shows the owner and repository of a Git URL without .git', () => {
    expect(sourceLabel(source('git', 'https://github.com/org/repo.git'))).toBe('org/repo')
    expect(sourceLabel(source('git', 'https://github.com/org/repo'))).toBe('org/repo')
    expect(sourceLabel(source('git', 'ssh://git@github.com/org/repo.git'))).toBe('org/repo')
  })

  it('shows the repository path of an scp-like Git location', () => {
    expect(sourceLabel(source('git', 'git@github.com:org/repo.git'))).toBe('org/repo')
  })

  it('falls back to the raw location when it cannot be parsed', () => {
    expect(sourceLabel(source('marketplace', 'not a url'))).toBe('not a url')
    expect(sourceLabel(source('directory', 'file:///%E0%A4%A'))).toBe('file:///%E0%A4%A')
    expect(sourceLabel(source('git', 'https://'))).toBe('https://')
  })
})
