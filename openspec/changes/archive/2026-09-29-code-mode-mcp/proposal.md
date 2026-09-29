## Why

Code mode shipped before the OpenSpec authority existed: an opt-in local MCP
server (`agent-marketplace mcp`) exposing `search` and `execute` over a
sandboxed JavaScript runtime, harness registration through `codeMode.enabled`,
exact session-log identity evidence, and `promote` for turning dynamic use
into durable installs. Its contract lived only in CLI/config reference docs
and tests, so the spec audit could not check it.

## What Changes

- Backfill the canonical `code-mode-mcp` capability spec from observed
  implementation and test behavior. No runtime behavior changes.
- Bind every scenario to an existing passing test with
  `// spec: code-mode-mcp/<scenario-slug>` annotations.

## Capabilities

### New Capabilities

- `code-mode-mcp`: Opt-in registration, the two-tool stdio surface,
  read-only catalog search, declared-server execution, sandbox limits,
  skill-content safety, MCP server lifecycle security, session-log identity
  evidence, and promotion to a durable selection.

### Modified Capabilities

None.

## Impact

Specification and test annotations only. `cmd/agent-marketplace`,
`internal/codemode`, `internal/mcpclient`, `internal/config`, and
`internal/pipeline` receive comment-line annotations in existing tests; no
implementation code changes.
