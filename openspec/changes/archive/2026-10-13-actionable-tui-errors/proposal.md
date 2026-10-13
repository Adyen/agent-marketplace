## Why

After a successful operation, the TUI can fail to verify resource state and show only a bare message with no next step. Errors and warnings that already carry remediation keep showing it. Unclassified errors and Marketplace discovery errors show no recovery text at all, so users have to guess the next command. Failed operation errors also stay on the errors screen after a healthy reload; they clear only after a successful retry of that operation.

## What Changes

- Show a plain-language fallback with `agent-marketplace sync --check` when post-operation verification fails without a specific cause.
- Keep every original error cause and show its existing remediation first; add typed-cause and category-based next steps only when none exists.
- Give unclassified errors and discovery errors a safe generic or catalog next step, without config-specific wording.
- Direct users to their administrator for organization files and locks, and never suggest deleting or bypassing an invalid, unreadable, or mismatched acquisition lock.
- Direct users to plain `agent-marketplace sync` for interrupted writes.

## Capabilities

### New Capabilities

- `tui-error-recovery`: Plain-language recovery steps for TUI errors, warnings, and discovery failures.

### Modified Capabilities

None.

## Impact

`internal/tui` error rendering and sync reload failure handling, plus new TUI tests and one benchmark. No CLI, service, configuration, lock, or write-path changes. Error identities, operation results, and diagnostic lifetimes are unchanged.
