## Context

Logging and telemetry are needed before resource acquisition. See proposal.md and specs/configuration-settings/spec.md.

## Goals / Non-Goals

**Goals:** Resolve settings from validated authored layers without consulting remote acquisition locks.

**Non-Goals:** Change telemetry consent, routing precedence, or remote resource validation.

## Decisions

- Read the settings view from configuration intent. Using full resource binding at startup would make logging and telemetry depend on unavailable remote evidence.
- Retain existing layer precedence when merging settings so lock-independent reads cannot silently select a lower-priority value.

## Risks / Trade-offs

- A layer with invalid authored settings still fails validation; absence of acquisition evidence is not a reason to skip authored validation.
