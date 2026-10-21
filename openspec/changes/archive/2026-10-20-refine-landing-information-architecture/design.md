## Context

The Landing redesign now has accessible tab and copy primitives. Its scope
section still reveals one choice at a time, while the harness fixture displays
Factory before the intentionally selected Claude default.

## Goals / Non-Goals

**Goals:**
- Make scope outcomes directly comparable.
- Keep CLI scope values and the default example command clear.
- Align visible harness order with the existing default.
- Keep the organization preview compact.

**Non-Goals:**
- Change actual CLI scope or harness defaults.
- Change supported harness data or Marketplace installation behavior.

## Decisions

Scopes use three informational columns in one quiet panel. Inset dividers
preserve comparison structure without implying that the columns are controls.
Each column exposes its CLI value, while Repository carries the default label
and one valid static example command. Harness data is reordered without
changing any entry so Claude leads the tabs it already defaulted.
Depiction-only labels are shortened without changing destinations or actions.
Icons that communicate UI actions use filled silhouettes for more visual
weight. Explanatory icons paired with prose remain outlined so they continue
to read as supporting content rather than controls.

## Risks / Trade-offs

Showing all scope details uses more vertical space on narrow screens. Columns
stack with horizontal inset dividers to preserve complete text.

## Migration Plan

Apply the Landing-only refinements, remove obsolete compatibility tokens,
verify the comparison witnesses, and archive the change.
