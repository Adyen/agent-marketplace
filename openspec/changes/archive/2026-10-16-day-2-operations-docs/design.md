## Context

See `proposal.md`. The facts come from the governing specifications, the four fix branches, and command transcripts from a fixed binary run in a Linux container with the day-2 experiment scripts.

## Goals / Non-Goals

Goals: Diátaxis pages in plain English that describe current behavior, with commands paired with real output.

Non-goals: runtime or spec changes; the existing broken anchors `#before-following-a-journey` and `#example-nginx-configuration`; TUI-started update behavior.

## Decisions

- Describe fixed behavior only. A page does not mention the old behavior as current.
- Output blocks come from transcripts of the fixed binary, trimmed only where the page says so.
- New pages join the existing sidebar groups instead of adding groups.

## Risks / Trade-offs

- A fix pull request could change before it merges. The pages cite output from the reviewed fix heads, and the docs pull request states its dependency on them.

## Migration Plan

Publish through the normal site build. No runtime migration is needed.
