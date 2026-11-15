# Design

## Context

See proposal.md - Why. The updated behavior contract is in `specs/gui-shell/spec.md`. `gui/DESIGN.md` describes the workspace toolbar, status bar, inspector, modals, navigation, and diagnostics. The reported sticky-header issue has not been reproduced.

The change stays in the Vue interface. It does not change the local API, stored configuration, or data model.

## Goals / Non-Goals

**Goals:**

- Keep presentation changes local to the components that render them.
- Keep diagnostic copying based on the diagnostic data already passed to the interface.
- Make the copy action and multi-message expansion separate from selectable diagnostic text.
- Change sticky-header CSS only if a reproduction provides evidence for a specific fix.

**Non-Goals:**

- Change the local API or stored configuration.
- Add file watching.

## Decisions

- Keep refresh on Command-R and remove its visible toolbar control. Retain the existing focus and post-mutation reload paths rather than adding a second reload mechanism.
- Build copied diagnostic text from the supplied diagnostic props in input order. Format each entry as `<severity> <code>: <message>` and join entries with one newline. The displayed diagnostics are grouped by severity, so copied text does not follow display order. This avoids an API change.
- Prefer the native `ClipboardSetText` bridge. Use `navigator.clipboard.writeText` only when the native bridge is unavailable. Show "Copied" only after the chosen write call resolves successfully.
- Use a separately named native button for expanding multiple messages. Keep the message text in a sibling element so users can select it without activating the button. Enter and Space operate the native button.
- Reproduce the sticky-header report before editing CSS. If it reproduces, change only the smallest component-local rule supported by the evidence. If it does not, make no sticky-header CSS change.
- Keep the remaining workspace presentation cleanup within its owning components. Choose CSS changes from matched before-and-after evidence rather than extending global styles.

## Risks / Trade-offs

- [The native bridge may be unavailable in a browser preview] → Use the browser clipboard API only when `ClipboardSetText` is unavailable.
- [Clipboard writes may fail] → Show "Copied" only after the write call resolves.
- [The sticky-header issue may not reproduce] → Record the attempt and make no CSS change without a reproduction.
- [A visual adjustment may affect another workspace state] → Keep the rule component-local and compare the same state before and after.

## Migration Plan

No migration is needed. The change does not alter stored data or API contracts. Reverting the interface changes restores the prior presentation.
