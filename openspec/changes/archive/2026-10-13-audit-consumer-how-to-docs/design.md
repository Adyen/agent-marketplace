## Context

The guides serve competent users carrying out specific tasks. See `proposal.md` for the audit scope. Governing specifications take precedence over code, and code and tests define facts where specifications do not.

## Goals / Non-Goals

Keep steps and recovery guidance in each guide. Link to existing reference and explanation pages rather than copying inventories or rationale. Do not change behavior to make a documented example work.

## Decisions

- Preserve existing page URLs by default to avoid unnecessary navigation churn.
- Record each page's reader, quadrant, and source checks in private audit evidence rather than adding process notes to public pages.
- Label variable command output as typical. Describe successful commands that produce no output rather than inventing messages.
- Run practical probes in isolated temporary homes through `go run`, with telemetry disabled. Run `make build` separately. Record probes and limitations; source review covers cases that require unavailable services or credentials.
- Keep online Marketplace contact distinct from exact-Git cached checks, and retain failure, ownership, scope, and recovery limits.

## Risks / Trade-offs

- Environment-dependent output can mislead readers. Label examples as typical and retain their source or runtime evidence.
- A passing reference contract test does not audit every how-to claim. Pair it with the full page audit and site build.
- An outside page may retain an unrelated error. Keep substantive changes within this section and route other defects to their owners.

## Migration Plan

Publish the reviewed documentation as a stacked change. No consumer migration is required; Git can restore the previous prose if needed.
