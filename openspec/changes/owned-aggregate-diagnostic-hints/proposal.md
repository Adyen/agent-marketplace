# Proposal

## Why

Diagnostics that Agent Marketplace reports while reconciling owned aggregate files (Devin, Codex, Claude settings, Gemini, OpenCode and native hook aggregates) reach the CLI without the guidance catalogued for their code. `sync` then prints a blank `  → ` remediation line and falls back to the generic `sync --check` hint, which breaks the existing rule that every printed error names a code-specific next step.

## What Changes

- Owned-aggregate diagnostics carry the catalogued hint for their stable code, so `sync` prints that guidance as the remediation line and as the `hint:` line.
- Warning and error output never prints an empty remediation line when an issue has no remediation.
- Every code reported through the owned-aggregate path has code-specific guidance, enforced by a test.
- No diagnostic code, severity, message text, exit status, or hint layout changes.

## Capabilities

### New Capabilities

### Modified Capabilities
- `command-error-output`: "Printed errors name a next step" gains scenarios for owned-aggregate diagnostics and for issues without remediation.

## Impact

`internal/materialize` (owned-aggregate diagnostic construction), `cmd/agent-marketplace` warning and error rendering, and `internal/remedy` guidance coverage. No API, configuration, lock, or machine-readable output change.

## Release note

When `sync` cannot safely reconcile a harness aggregate file such as `.devin/hooks.v1.json`, it now prints the guidance for that diagnostic code instead of an empty `→` line and a generic `sync --check` hint.
