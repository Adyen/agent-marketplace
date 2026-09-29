## Context

This is a historical backfill: the implementation predates the OpenSpec
authority. Requirements were written from the current source of truth —
`cmd/agent-marketplace/mcp.go`, `promote.go`, `internal/codemode`,
`internal/mcpclient`, `internal/config` `codeMode` handling, and
`internal/pipeline` registration — cross-checked against the CLI and
configuration reference docs. Every scenario is bound to an existing
annotated test that already passes.

## Decisions

- **Spec reflects observed behavior, not intended behavior.** Limits
  (30 s deadline, 1 MiB output cap, 100 host calls), error codes
  (`codemode.*`, `resource.*`, `mcp.*`, `catalog.stale`), and lifecycle
  rules (dead servers never restart, declared env only, https-or-loopback
  remote endpoints) are specified exactly as implemented.
- **Registration is config-owned, not harness-owned.** `codeMode.enabled`
  is a v4-only block; sync injects a required owned `agent-marketplace`
  MCP artifact into the resolved set so ordinary owned-entry
  reconciliation registers and unregisters it in every target harness.
- **Promotion replays recorded provenance.** The session log under
  `$XDG_STATE_HOME/agent-marketplace/mcp-sessions/` is the only evidence
  `promote` trusts; unprovenanced or ambiguous records fail closed.

## Discarded claims

- Any claim that code mode is on by default or registers unconditionally:
  contradicted by `codeModeMCPServer` gating on the merged enable flag.
- Any claim of network fetch, subprocess spawn, or file write inside the
  `search` sandbox: contradicted by the catalog-only binding.

## Risks

- Spec scenarios describe stable coded errors; renaming an error code
  without updating the spec will fail traceability review.
