## Why

Landing's scope and harness choices are harder to scan than necessary: scope
details are hidden behind tabs, and the selected default harness does not lead
the tab order. The organization preview also carries labels that are too long
for the compact product depiction.

## What Changes

- Show all installation scopes as comparable selectable cards.
- Keep repository scope selected by default and update the example command when
  another scope is chosen.
- Put Claude Code first so the selected default leads the harness tabs.
- Tighten labels in the organization Marketplace depiction.
- Remove superseded compatibility token definitions after every Landing
  component uses the public-site roles.

## Capabilities

### Modified Capabilities

- `marketplace-web-ui`: Landing's scope, harness, and organization depictions
  gain an explicit information hierarchy.

## Impact

The change affects Landing-only fixtures, content presentation, and controls.
It does not change CLI defaults, harness support, installation semantics, or
Marketplace application behavior.
