## Context

The shared native settings pass moves repository fragments to a machine output. That output also runs ordinary artifact plans, whose empty desired set prunes newly written repository commands.

## Goals / Non-Goals

- Preserve ordinary repository resources while keeping one combined native settings reconciliation.
- Retain byte/mtime-neutral checks, materialization modes, filesystem aliases, and stale-entry cleanup.
- Do not combine independently selected ordinary resource plans across scopes or change HOME configuration loading.

## Decisions

Mark the extra machine work as native-only when it has no independently compiled or blocked machine work and repository output is active. Execute its native aggregate reconciliation but skip ordinary artifact and skill plans. Keep the flag false for ordinary machine-only cleanup after the last repository selection is removed.

Use the existing shared native synchronization benchmark to measure the scheduling change. Test command preservation through the real materialization entry point, with and without native fragments, in both materialization modes and through a HOME alias.

## Risks / Trade-offs

Do not suppress cleanup for every shared machine output. Independently selected machine work retains its existing plans, and an empty desired set must still remove stale owned output. Incomplete input retains the existing conservative native prune fence.
