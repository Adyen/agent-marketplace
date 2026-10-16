## Context

See proposal.md for motivation. The reference audit spans consumer, registry-author, and Marketplace-operator facts, with existing TestDocsContract headings as a compatibility constraint.

## Goals / Non-Goals

Keep reference pages focused on lookup facts. Leave behavior, tests, normative specifications, and prose outside the reference section unchanged, except necessary navigation and inbound-link fixes.

## Decisions

- Preserve all existing reference paths by default. This avoids unnecessary navigation and inbound-link changes; page removal requires specific confirmation.
- Use plain fact tables and minimal syntax examples instead of procedural steps. Link to existing how-to and explanation pages rather than duplicating them.
- Treat governing specifications as authority, then current source and tests where no specification exists. Do not import unmerged feature behavior.
- Keep the per-page quadrant, reader, source references, and practical command captures in private evidence. Public docs contain product facts, not audit bookkeeping.
- Build with make build, but run safe command probes with go run in isolated HOME and state roots. This respects the execution rule without mutating host configuration.

## Risks / Trade-offs

- Dense references can lose failure conditions during shortening. Retain defaults, constraints, refusal behavior, and exit semantics, and reconcile them in documentation review.
- Help-only contract checks do not prove all facts. Use per-page source references and focused sandbox probes alongside the contract test and dead-link build.
- Literal command output may include an em dash. Preserve faithful output while removing em dashes from prose.
