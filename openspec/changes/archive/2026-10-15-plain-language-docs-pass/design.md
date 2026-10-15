## Context

See `proposal.md` for scope and motivation. The facts are fixed by the current runtime, the governing specifications, and the `openspec/config.yaml` glossary. Prose edits must leave them unchanged.

## Goals / Non-Goals

Goals: shorter, plainer prose on each page, in the same Diátaxis quadrant and with the same reader.

Non-goals: changing facts, moving or renaming pages, changing headings that other pages link to, and editing code, examples, or generated content.

## Decisions

- Cut before rewriting. Delete throat-clearing openers, sentences that restate a table or heading, and filler hedges. Then shorten what remains.
- Short words over long ones, active voice where the actor is clear, and everyday words over jargon. Glossary terms stay exactly as the glossary spells them.
- Keep fenced code blocks, inline code spans, and literal output byte-identical. A check script compares them at the base and at each commit.
- Keep headings unless a rename is needed for plain English. Headings that other pages link to by anchor are not renamed.
- Remove em dashes from prose. Remove stock phrases such as "under the hood", "out of the box", "seamlessly", "leverage", and "first-class".
- Keep every sentence that might carry a condition, caveat, or diagnostic code. When unsure, keep it.

## Risks / Trade-offs

- Cutting prose can remove a caveat by accident. The per-area review compares each removed sentence with its replacement.
- A check of code blocks and inline spans does not prove the prose is still correct. Build, link, spec, and contract checks run alongside it.

## Migration Plan

Publish through the normal site build. No runtime migration or schema change is needed.
