import { describe, expect, it } from 'vitest'
import type { SourceDescription } from './api/client'
import { sourceLabel } from './source-label'

function source(kind: SourceDescription['kind'], location: string, name = ''): SourceDescription {
  return { id: 'id', name, kind, location, path: '', ref: '', sourceHarness: '' } as SourceDescription
}

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

  // spec: gui-shell/named-marketplace-source-shows-its-name
  it('uses a configured Marketplace source name', () => {
    expect(sourceLabel(source('marketplace', 'https://tools.example.com/acme', 'team-tools'))).toBe('team-tools')
  })

  // spec: gui-shell/generic-or-missing-name-falls-back-to-the-host
  it('falls back from missing or generic Marketplace names to the full endpoint host', () => {
    expect(sourceLabel(source('marketplace', 'https://agent-marketplace.is.example.com'))).toBe('agent-marketplace.is.example.com')
    expect(sourceLabel(source('marketplace', 'https://mp.example.com:8443/acme/', 'marketplace-2'))).toBe('mp.example.com:8443/acme')
    expect(sourceLabel(source('marketplace', 'https://marketplace.example/services/catalog', 'marketplace'))).toBe('marketplace.example/catalog')
    expect(sourceLabel(source('marketplace', 'http://localhost:8443/api'))).toBe('localhost:8443/api')
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
