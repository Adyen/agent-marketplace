## Context

The Landing redesign now has accessible tab and copy primitives. Its scope
section still reveals one choice at a time, while the harness fixture displays
Factory before the intentionally selected Claude default.

## Goals / Non-Goals

**Goals:**
- Make scope outcomes directly comparable.
- Keep selection semantics and generated example commands clear.
- Align visible harness order with the existing default.
- Keep the organization preview compact.

**Non-Goals:**
- Change actual CLI scope or harness defaults.
- Change supported harness data or Marketplace installation behavior.

## Decisions

Scopes use native radio inputs inside visible cards, preserving browser keyboard
behavior and one selected value. All three outcomes remain visible; selection
only changes the example command. The checked repository scope remains the
default. Harness data is reordered without changing any entry so Claude leads
the tabs it already defaulted. Depiction-only labels are shortened without
changing destinations or actions.

## Risks / Trade-offs

Showing all scope details uses more vertical space on narrow screens. Cards
stack to preserve complete text and touch targets.

## Migration Plan

Apply the Landing-only refinements, remove obsolete compatibility tokens,
verify the selection witnesses, and archive the change.
