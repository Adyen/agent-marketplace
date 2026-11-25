import { describe, expect, it } from 'vitest'
import { type DiagnosticSeverity, groupDiagnostics } from './diagnostics'

function diagnostic(severity: DiagnosticSeverity, code: string, message: string) {
  return { severity, category: 'catalog', code, message }
}

describe('groupDiagnostics', () => {
  it('collapses one code with different messages into a group that keeps their order', () => {
    const groups = groupDiagnostics([
      diagnostic('warning', 'subagent.devin.model-default', 'alpha uses the default model'),
      diagnostic('warning', 'subagent.devin.model-default', 'beta uses the default model'),
    ])

    expect(groups).toEqual([
      {
        key: 'warning:subagent.devin.model-default',
        severity: 'warning',
        messages: ['alpha uses the default model', 'beta uses the default model'],
      },
    ])
  })

  it('orders errors, then warnings, then info, keeping first appearance within a severity', () => {
    const groups = groupDiagnostics([
      diagnostic('info', 'i1', 'info one'),
      diagnostic('warning', 'w1', 'warning one'),
      diagnostic('error', 'e1', 'error one'),
      diagnostic('warning', 'w2', 'warning two'),
      diagnostic('error', 'e2', 'error two'),
    ])

    expect(groups.map((group) => group.key)).toEqual(['error:e1', 'error:e2', 'warning:w1', 'warning:w2', 'info:i1'])
  })

  it('keeps the same code at different severities in separate groups', () => {
    const groups = groupDiagnostics([
      diagnostic('info', 'shared', 'as info'),
      diagnostic('error', 'shared', 'as error'),
    ])

    expect(groups.map((group) => [group.severity, group.messages])).toEqual([
      ['error', ['as error']],
      ['info', ['as info']],
    ])
  })
})
