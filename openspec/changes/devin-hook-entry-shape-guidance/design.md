# Design

## Context

Every owned-aggregate shape refusal is built by one constructor in `internal/materialize`, which copies the catalog hint for its code. The entry-level refusal comes from the shared existing-entry reader used by every JSON hook aggregate; the Devin root, member, and event refusals share the same Devin code. The CLI prints a diagnostic's own nonempty hint as its `hint:` line ahead of the code catalog.

## Goals / Non-Goals

**Goals:**
- Give the Devin entry-level refusal an entry-specific hint without changing its code or message.
- Keep root, member, and event refusals on the catalog hint.

**Non-Goals:**
- A new diagnostic code, catalog rewording, or changes to Codex and native-hook guidance.
- Any automatic rewrite, adoption, or migration of non-matching entries.

## Decisions

- **Message-specific hint under the existing code.** The entry reader returns a typed error carrying the event and index, with its message unchanged. The shape-diagnostic constructor replaces the catalog hint with the entry hint only for `materialize.devin.aggregate-shape` entry errors. Alternative: a new `materialize.devin.hook-entry-shape` code; rejected because it changes a stable, published code for an existing refusal and would need parallel codes for other harnesses.
- **Catalog unchanged.** `diag/codes.go` and `diag/codes.md` keep their identical root/member hint, which stays accurate for every remaining case.
- **Devin only.** Codex and native-hook catalog hints tell the user to fix the reported file by hand; they are vague but not wrong, so they do not share the gap.

## Risks / Trade-offs

- [The entry hint varies per entry under one code] → the CLI deduplicates on code, resource, source, and hint, so distinct entries stay distinct rather than collapsing.
