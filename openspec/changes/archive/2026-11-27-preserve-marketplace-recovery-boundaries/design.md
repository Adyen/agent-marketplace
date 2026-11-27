# Design

## Context

See proposal.md for motivation. The existing recovery and removal flows already have regression coverage; the change records their selection-precedence and hook-evidence boundaries as normative behavior.

## Goals / Non-Goals

**Goals:**

- Keep recovery scoped to the current effective selection, matched by exact source and locator.
- Limit the unrelated-hook exception to an unrecorded optional direct non-hook selection without an explicit revision.
- Preserve the evidence requirement for hook and hook-containing collection removal when active native hooks may be affected.
- Bind each new scenario to its existing regression test.

**Non-Goals:**

- Change runtime behavior or broaden removal to hooks or collections without prior-selection evidence.
- Change the configuration, acquisition-lock, or ownership-state formats.

## Decisions

- A resource-level 404 skips only the effective root selection. The root selection remains the winner for its exact source and locator; resolution does not fall through to a superseded fragment pin.
- Before-state removal preflight may skip missing-evidence checks only for the matched non-hook direct leaf. A hook or collection that may affect active native hooks continues to require prior-selection evidence.
- Accepted removal does not request the removed resource or change unrelated handwritten native hooks. The remaining selections still synchronize as usual. A refused hook or collection removal leaves configuration, native hook files, absent locks, and ownership state unchanged.
- Broader fallthrough after a missing root selection was rejected because it would restore a superseded pin. Skipping hook-evidence checks for every removal was rejected because it could authorize changes to active hook output without proof.

## Risks / Trade-offs

- An overly broad exception could weaken hook safety. The spec limits it to an unrecorded optional direct non-hook selection without an explicit revision; hook and collection cases retain their evidence requirement.
- Name-only matching could confuse selections from different sources. Recovery is explicitly bound to the exact source and locator.
