# Design

## Context

The Diagnostics view currently renders its copy control independently of the diagnostics list. The copy handler supports both the native bridge and the browser clipboard.

## Goals / Non-Goals

**Goals:**

- Expose the empty-list state through the rendered control's disabled state.
- Preserve the existing copy behavior when diagnostics are present.

**Non-Goals:**

- Change clipboard selection, write-result handling, or diagnostic formatting.
- Add a handler guard or refactor clipboard behavior.

## Decisions

Bind the rendered button's `disabled` state to whether the diagnostics list is empty. A handler guard alone would leave an empty copy action presented as available. Test the rendered control and attempt a normal click with both clipboard routes available.

## Risks / Trade-offs

- A regression could leave the empty control enabled or allow clipboard writes. The workspace test checks the disabled state, unchanged label, and both clipboard spies.
