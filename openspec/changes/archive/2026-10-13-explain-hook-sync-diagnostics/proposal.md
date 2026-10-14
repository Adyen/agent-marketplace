## Why

`sync` and `sync --check` report hook compilation errors without the failing target, so the same hook failing for two harnesses prints one ambiguous line. A matcher that cannot be decoded and a matcher that the target cannot cover print the same cause and remedy. Hook review labels and drift summaries also do not say what operation is pending.

## What Changes

- Show the failing target for hook compilation error diagnostics and keep same-target repeats collapsed.
- Give decode failures and target-coverage failures distinct static causes and hints, keeping the existing code and suppression.
- Label pending hook review entries as add, replace, or remove, and count drift as writes, removals, and outputs.

This is a user-visible diagnostic-only change. It does not change hook translation, publication, ownership, config, lock, or native writing behavior.
