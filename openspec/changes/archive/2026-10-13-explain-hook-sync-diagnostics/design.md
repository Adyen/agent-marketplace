## Context

Hook compiler error diagnostics already carry `Target`, but the promoted sync error drops it and the dedup identity ignores it. The resolver reports decode and coverage failures with one shared message. `sync --check` prints error diagnostics raw. Hook review prints the change target, scope, file, event, and matcher, followed by the before and after payload lines, but no action label.

## Decisions

- Target qualification applies only to `hook.`-prefixed error diagnostics with a target. The qualifier is rendered inside the existing parenthesized identity and included in the dedup identity for those errors only, so non-hook and warning output is unchanged.
- Decode and coverage refusals use distinct static messages and hints in `resolve.go`. Neither exposes the matcher or any error text.
- Hook review derives add, replace, or remove from the before and after payloads. The pending-removal explanation prints only for a pending removal, not for an applied one, and states that owned entries and support files are removed without changing ownership.
- Drift summaries use the generic output vocabulary: writes, removals, and outputs.

## Non-goals

No matcher parsing changes, no catalog changes, no new diagnostic codes, and no new public data types or source-provenance APIs.
