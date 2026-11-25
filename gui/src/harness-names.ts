const harnessNames: Record<string, string> = {
  claude: 'Claude Code',
  factory: 'Factory',
  cursor: 'Cursor',
  windsurf: 'Windsurf',
  gemini: 'Gemini CLI',
  devin: 'Devin',
  codex: 'Codex',
  opencode: 'OpenCode',
}

export function harnessDisplayName(id: string): string {
  return harnessNames[id] ?? id
}
