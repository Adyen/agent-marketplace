export type DiagnosticSeverity = 'error' | 'warning' | 'info'

export interface DiagnosticGroup {
  key: string
  severity: DiagnosticSeverity
  messages: string[]
}

const SEVERITY_RANK: Record<DiagnosticSeverity, number> = { error: 0, warning: 1, info: 2 }

export function groupDiagnostics(diagnostics: { severity: DiagnosticSeverity; code: string; message: string }[]): DiagnosticGroup[] {
  const groups = new Map<string, DiagnosticGroup>()
  for (const diagnostic of diagnostics) {
    const key = `${diagnostic.severity}:${diagnostic.code}`
    const group = groups.get(key)
    if (group) {
      group.messages.push(diagnostic.message)
    } else {
      groups.set(key, { key, severity: diagnostic.severity, messages: [diagnostic.message] })
    }
  }
  return [...groups.values()].sort((a, b) => SEVERITY_RANK[a.severity] - SEVERITY_RANK[b.severity])
}
