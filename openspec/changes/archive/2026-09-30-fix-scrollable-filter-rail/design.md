## Context

The desktop catalog places the filter controls in a sticky container offset below the shared navigation. The container currently has no height bound or overflow behavior, so content below the viewport cannot become visible once a long rail reaches its sticky position. See `proposal.md` for motivation and `specs/marketplace-browser/spec.md` for the observable contract.

## Goals / Non-Goals

**Goals:**

- Preserve the desktop rail's sticky behavior while making every control reachable.
- Derive the rail's available height from the viewport and its existing sticky offset.
- Keep mobile filter behavior unchanged.

**Non-Goals:**

- Changing filter selection, registry truncation, or search behavior.
- Introducing custom scrollbar styling or JavaScript-managed scrolling.

## Decisions

Bound the sticky rail using CSS and set vertical overflow to automatic. The maximum height will subtract the sticky top offset and a bottom gutter from the viewport height, keeping the scrolling region visibly separated from the viewport edge. Native overflow preserves keyboard, wheel, and touchpad scrolling without additional state or event handlers.

Removing sticky positioning was rejected because it would make filters unavailable while users browse long result lists. Relying on page scrolling alone was rejected because an over-height sticky element anchored at a fixed top offset cannot expose its lower controls reliably.

The regression test will provide enough registry controls to exceed a desktop viewport, verify that overflow is enabled and the rail is viewport-bounded, then scroll the rail's final control into view. This tests the actual consumer path rather than only checking declared CSS.

## Risks / Trade-offs

- [Nested scrolling can be less obvious than page scrolling] → Use native automatic overflow only when content exceeds the available height; shorter rails retain their current presentation.
- [A hard-coded viewport subtraction can drift from the sticky offset] → Express both values together in the same desktop media-query rule and test the resulting geometry.

## Migration Plan

Deploy as a CSS-only browser update with its regression test. Rollback is the removal of the height and overflow declarations; no persisted data or API compatibility is involved.
