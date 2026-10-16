import { configuration, row, snapshot } from '../src/fixtures.ts'
import type { Configuration, Snapshot } from '../src/api/client.ts'
import type { Row } from '../src/controller.ts'

export interface PreviewScenario {
  snapshot: Snapshot
  configuration: Configuration
}

const workspaceRows: Row[] = [
  row('audit-trail', {
    id: 'organization/platform-core/skill/audit-trail',
    kind: 'skill',
    scope: 'organization',
    registry: 'platform-core',
    required: true,
    group: 'required',
    status: 'required (org)',
    harnesses: ['devin', 'claude'],
    description:
      'Records every tool call, sync decision, and resource change made through the agent so that platform security reviews can reconstruct what happened during a session. Required by organization policy; it cannot be disabled from user scope.',
  }),
  row('secret-scan', {
    id: 'organization/platform-core/skill/secret-scan',
    scope: 'organization',
    registry: 'platform-core',
    required: true,
    group: 'required',
    status: 'required (org)',
    harnesses: ['devin', 'claude'],
    description: 'Blocks commits that contain credentials or key material.',
  }),
  row('code-review', {
    id: 'user/team-tools/skill/code-review',
    registry: 'team-tools',
    description: 'Reviews a diff for correctness, security, and style issues before it is pushed.',
    harnesses: ['devin', 'claude'],
  }),
  row('commit-message', {
    id: 'user/team-tools/skill/commit-message',
    registry: 'team-tools',
    harnesses: ['devin', 'claude'],
  }),
  row('changelog', {
    id: 'user/team-tools/command/changelog',
    kind: 'command',
    registry: 'team-tools',
    harnessOverride: true,
    harnesses: ['devin'],
    description: 'Drafts a changelog entry from the commits since the last tag.',
  }),
  row('explorer', {
    id: 'user/team-tools/subagent/explorer',
    kind: 'subagent',
    registry: 'team-tools',
    harnesses: ['devin', 'claude'],
    description: 'Read-only codebase exploration subagent for broad questions.',
  }),
  row('pre-commit', {
    id: 'local/local-dev/hook/pre-commit',
    kind: 'hook',
    scope: 'local',
    registry: 'local-dev',
    harnesses: ['devin', 'claude'],
    description: 'Runs the formatter and linter on staged files.',
  }),
  row('github', {
    id: 'project/team-tools/mcpserver/github',
    kind: 'mcpserver',
    scope: 'project',
    registry: 'team-tools',
    harnesses: ['devin', 'claude'],
    description: 'GitHub issues and pull request tools.',
  }),
  row('k8s-debug', {
    id: 'user/agent-skills-hub/skill/k8s-debug',
    scope: 'user',
    registry: 'agent-skills-hub',
    installedFiles: true,
    resourceAction: 'remove',
    harnesses: ['devin', 'claude'],
    description: 'Walks through pod, network, and rollout failures in a Kubernetes cluster.',
  }),
  row('release-notes', {
    id: 'user/local-dev/skill/release-notes',
    registry: 'local-dev',
    harnesses: ['devin', 'claude'],
  }),
  row('sql-review', {
    id: 'user/local-dev/skill/sql-review',
    registry: 'local-dev',
    harnesses: ['devin', 'claude'],
  }),
  row('test-planner', {
    id: 'project/team-tools/skill/test-planner',
    scope: 'project',
    registry: 'team-tools',
    harnesses: ['devin', 'claude'],
  }),
  row('cargo-deps', {
    id: 'user/local-dev/skill/cargo-deps',
    registry: 'local-dev',
    group: 'pending',
    status: 'enabled (pending sync)',
    harnesses: ['devin', 'claude'],
  }),
  row('docs-sync', {
    id: 'user/team-tools/skill/docs-sync',
    registry: 'team-tools',
    group: 'pending',
    status: 'enabled (pending sync)',
    harnesses: ['devin', 'claude'],
  }),
  row('lint-fixer', {
    id: 'user/local-dev/skill/lint-fixer',
    registry: 'local-dev',
    group: 'stale',
    status: 'stale',
    harnesses: ['devin', 'claude'],
  }),
  row('legacy-deploy', {
    id: 'user/local-dev/skill/legacy-deploy',
    registry: 'local-dev',
    group: 'broken',
    status: 'broken: missing SKILL.md',
    checked: false,
    harnesses: ['devin', 'claude'],
  }),
  row('tf-plan-summary', {
    id: 'user/local-dev/skill/tf-plan-summary',
    registry: 'local-dev',
    group: 'malformed',
    status: 'malformed: bad frontmatter',
    checked: false,
    harnesses: ['devin', 'claude'],
  }),
  row('incident-summary', {
    id: 'available/the-enterprise-wide-shared-agent-resources-registry-for-all-platform-teams/skill/incident-summary',
    scope: 'available',
    registry: 'the-enterprise-wide-shared-agent-resources-registry-for-all-platform-teams',
    group: 'available',
    status: 'available',
    checked: false,
    discovery: true,
    description: 'Summarizes an incident timeline from chat, alerts, and tickets into a postmortem draft.',
  }),
  row('rust-review', {
    id: 'available/agent-skills-hub/skill/rust-review',
    scope: 'available',
    registry: 'agent-skills-hub',
    group: 'available',
    status: 'available',
    checked: false,
    discovery: true,
    inspectable: true,
    resourceAction: 'install',
  }),
  row('api-contract-check', {
    id: 'available/agent-skills-hub/skill/api-contract-check',
    scope: 'available',
    registry: 'agent-skills-hub',
    group: 'available',
    status: 'available',
    checked: false,
    discovery: true,
    inspectable: true,
  }),
]

const workspaceDiagnostics: Snapshot['diagnostics'] = [
  {
    severity: 'error',
    category: 'parse',
    code: 'frontmatter',
    message:
      'Could not parse frontmatter in /home/dev/workspaces/agent-platform/resources/skills/legacy-deploy/SKILL.md: mapping values are not allowed in this context at line 3 column 14',
  },
  {
    severity: 'warning',
    category: 'lock',
    code: 'stale-lock',
    message:
      'Lock file /home/dev/workspaces/agent-platform/.agents/lock.yaml is older than the configured source revision; run sync to refresh the materialized resources.',
  },
  {
    severity: 'warning',
    category: 'source',
    code: 'git-ref',
    message: 'Git source https://git.example.com/platform/agent-skills.git declares ref "main", which resolves to a moving branch head.',
  },
  {
    severity: 'warning',
    category: 'resource',
    code: 'shadowed',
    message: 'Resource skill/code-review from registry team-tools is shadowed by /etc/agent-marketplace/registry/skills/code-review.',
  },
  {
    severity: 'warning',
    category: 'resource',
    code: 'shadowed',
    message: 'Resource skill/commit-message from registry team-tools is shadowed by /etc/agent-marketplace/registry/skills/commit-message.',
  },
  {
    severity: 'warning',
    category: 'resource',
    code: 'shadowed',
    message: 'Resource skill/sql-review from registry local-dev is shadowed by /etc/agent-marketplace/registry/skills/sql-review.',
  },
  {
    severity: 'error',
    category: 'install',
    code: 'hook-target',
    message: 'Hook pre-commit could not be written to /home/dev/.claude/settings.json: file is managed outside the GUI.',
  },
]

const defaultScenario = (): PreviewScenario => ({
  snapshot: snapshot(workspaceRows, {
    diagnostics: workspaceDiagnostics,
    harnesses: ['devin', 'claude'],
    pendingChanges: 2,
  }),
  configuration: configuration({
    userTargets: ['devin', 'claude'],
    organizationTargets: ['devin'],
    supportedTargets: ['devin', 'claude', 'codex'],
    userSources: [
      { id: 'src-local', kind: 'directory', location: '/home/dev/workspaces/agent-platform/resources/skills', path: '', ref: '', sourceHarness: '' },
      { id: 'src-team', kind: 'git', location: 'https://git.example.com/platform/agent-skills.git', path: 'skills', ref: 'main', sourceHarness: '' },
      { id: 'src-hub', kind: 'marketplace', location: 'https://marketplace.example.com/api', path: '', ref: '', sourceHarness: '' },
    ],
    organizationSources: [
      { id: 'src-org', kind: 'directory', location: '/etc/agent-marketplace/registry', path: '', ref: '', sourceHarness: '' },
    ],
  }),
})

const emptyScenario = (): PreviewScenario => ({
  snapshot: snapshot(),
  configuration: configuration(),
})

export const scenarios: Record<string, () => PreviewScenario> = {
  default: defaultScenario,
  empty: emptyScenario,
}

export function scenario(name: string): PreviewScenario {
  if (!Object.hasOwn(scenarios, name)) {
    throw new Error(`unknown preview scenario "${name}"; valid scenarios: ${Object.keys(scenarios).join(', ')}`)
  }
  return scenarios[name]()
}
