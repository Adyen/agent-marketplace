## Context

The same read-only inspection issues appear in status, sync check, and terminal UI. See proposal.md and specs/inspection-outcomes/spec.md.

## Goals / Non-Goals

**Goals:** Apply one health classification to all inspection consumers while retaining each command's output format.

**Non-Goals:** Change materialization, recover malformed aggregates, or hide warning diagnostics.

## Decisions

- Classify diagnostic severity once before rendering it for individual consumers. Independent severity inference caused contradictory exit results.
- Preserve warning-only success even if there are no materialized resources. Treat an unsafe aggregate as an error without writing its file during a check.

## Risks / Trade-offs

- A consumer could suppress a classified issue when formatting; regression tests exercise the CLI and terminal UI paths as well as classification.
