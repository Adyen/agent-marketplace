# Design

## Context

Owned-aggregate diagnostics are built by one constructor in `internal/materialize`, which already reads the code's catalogue entry for its severity but drops its hint. The CLI renders a promoted diagnostic's hint through `resolver.Remediable`, and `remedy.For` prefers a nonempty remediation over its own code catalogue. Two single-issue print paths in `cmd/agent-marketplace/warnings.go` print the remediation arrow without checking for an empty string.

## Goals / Non-Goals

**Goals:**
- Copy the catalogued hint at the single construction point.
- Skip empty remediation in every warning and error print path.
- Guard coverage of every code the owned-aggregate path can emit.

**Non-Goals:**
- Rewording existing hints, reordering hint output, or changing codes, severities, messages, or exit status.

## Decisions

- **Copy the hint where severity is already read.** One constructor serves every owned-aggregate caller, so the fix is local and needs no per-caller change. Alternative: add remedy catalogue entries for every `materialize.*` code; rejected because the harness diagnostic catalogue already holds the guidance and would be duplicated.
- **Guard by static scan.** The guard lives beside the existing consumer-code coverage test in `internal/remedy`, which can see the remedy catalogue. It parses the non-test sources of `internal/materialize`, collects the code argument of every owned-aggregate constructor call and every `*Code` field value in reconcile option literals, resolves constants from the diagnostic catalogue package and the materialize package, and fails when a resolved code has neither a catalogue hint nor a remedy entry, or when a code argument cannot be resolved statically. Codes that only arrive at runtime inside a coded error are outside the scan.
- **Codes without catalogue guidance get a remedy entry.** Literal codes such as `materialize.write.error` that have no harness catalogue entry receive code-specific remedy guidance rather than a new harness catalogue entry, keeping published code documentation unchanged.

## Risks / Trade-offs

- [A future caller passes a code computed at runtime] → the guard fails on any unresolvable argument, forcing an explicit decision.
