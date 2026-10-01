## Context

This branch created separate OpenSpec changes for sticky navigation, footer styling, and iterative resource-detail refinements. The UI implementation is now reviewed as one Marketplace experience, while the separate archives repeat proposal, design, and task context.

## Goals / Non-Goals

**Goals:**
- Retain one compact capability for the branch's user-visible Marketplace UI contracts.
- Keep only scenarios backed by useful surviving tests.
- Remove superseded branch-local OpenSpec artifacts.

**Non-Goals:**
- Change the UI implementation during consolidation.
- Add test cases solely to preserve discarded micro-level visual contracts.
- Change Marketplace API or storage behavior.

## Decisions

Use one `marketplace-web-ui` capability covering persistent chrome, footer identity, and primary resource installation states. Keep browser coverage where the browser is essential, such as sticky positioning, and component coverage for resource state transitions. Do not retain exact spacing, typography, tooltip, or overflow-measurement scenarios after their dedicated tests were intentionally removed.

Archive one change containing this proposal, design, tasks, and delta specification. Delete the superseded navigation, chrome, and resource-detail change archives and their generated main specifications before applying the consolidated spec.

## Risks / Trade-offs

- [The consolidated capability is broader] → Keep requirements grouped by visible surface and avoid implementation details.
- [Some visual behavior is no longer normative] → Preserve only behavior considered valuable enough to maintain with tests.

## Migration Plan

Create and validate the consolidated change, remove the superseded branch-local changes and specs, archive the consolidated change, then run strict OpenSpec traceability.
