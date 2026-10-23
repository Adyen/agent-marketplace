## Why

The redesigned Landing needs deliberate motion and clearer interaction feedback
without making content or controls depend on animation.

## What Changes

- Add one-time hero arrival and section-eyebrow decode motion.
- Roll primary action and successful copy labels without changing accessible names.
- Add a travelling indicator and complete keyboard navigation to harness tabs.
- Respect reduced-motion preferences while preserving every interaction.

## Capabilities

### Modified Capabilities

- `marketplace-web-ui`: Landing gains accessible motion, copy feedback, and tab
  navigation behavior.

## Impact

The change affects Landing components, composables, and browser tests. It does
not change routes, commands, copied command bytes, or Marketplace application
behavior.
