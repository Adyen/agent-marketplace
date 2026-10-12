## Context

See proposal.md for the motivation. Existing instructions and references already cover command execution and detailed field inventories.

## Goals / Non-Goals

Preserve the explanation URLs and conceptual coverage. Keep source, tests, dependencies, and normative specs read-only. Outside the explanation section, change only essential links or navigation.

## Decisions

- Keep each page focused on one conceptual question and identify its reader in the private audit. Links serve readers who need instructions or exhaustive reference facts.
- Keep the full sync pipeline in how-agent-marketplace-works. Other pages explain their own relationships without repeating that pipeline.
- Rewrite user-journeys as an account of selection, installation, checking, updates, and removal rather than a sequence of commands.
- Verify retained facts against existing specs and source. Documentation follows specs when they disagree; it does not change behavior.
- Retain audit notes and execution logs outside the repository. They support review without adding a second public reference inventory.

## Risks / Trade-offs

- Removing mixed-quadrant material can lose useful context. Preserve conceptual coverage and link to its existing practical or reference home.
- Documentation can overstate safety or reproducibility. Check refusal cases, offline limits, organization locks, and ownership boundaries explicitly.

## Migration Plan

Publish the static docs through the normal site build. Reverting the documentation commit restores the previous text without migrating runtime state.
