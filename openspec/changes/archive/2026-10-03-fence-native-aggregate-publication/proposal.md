## Why

Claude native marker-based aggregates check leaf shape and bytes by path, then publish through a separately reopened pathname. Actual repository/machine MCP updates overwrite replacement handwritten aggregates after reconciliation checks.

## What Changes

- Reuse strict captured native snapshots for aggregate reconciliation and ownership-marker inventory reads.
- Reuse captured byte publication for marker-based aggregates with full observed leaf/parent/staged checks.
- Pass actual harness boundary through native aggregate orchestration, retaining private creation, existing modes, JSONC comments/unrelated fields and no-op identity.

## Capabilities

### Modified Capabilities

- `materialization`: marker-based native aggregate read/publication authority.

## Impact

R2 child of #494. Gemini and durable-ownership adapter transactions/recovery remain separate next consumers, as do backup/restore and durable state/journals. No new arbitrary byte budgets, fsync policy, output transaction or writer-exclusion claim.
