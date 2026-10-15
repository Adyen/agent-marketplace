import { describe, expect, it } from 'vitest'
import type { SourceDescription } from './api/client'
import { sourceLabel } from './source-label'

function source(kind: SourceDescription['kind'], location: string): SourceDescription {
  return { id: 'id', kind, location, path: '', ref: '', sourceHarness: '' } as SourceDescription
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

  it('shows the host of a Marketplace endpoint, keeping any port', () => {
    expect(sourceLabel(source('marketplace', 'https://marketplace.example/services/catalog'))).toBe('marketplace.example')
    expect(sourceLabel(source('marketplace', 'http://localhost:8443/api'))).toBe('localhost:8443')
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
