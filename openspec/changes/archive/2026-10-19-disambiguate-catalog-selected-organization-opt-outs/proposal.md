## Why

PR #752's early ambiguity check sees explicitly included repository resources but
not resources selected from a catalog by `all`, tags, or collection membership.
It can therefore exclude an organization Marketplace resource without asking
which installation scope the user intended.

## What Changes

- Defer unqualified organization Marketplace resource opt-out to the normal
  resolver's candidate set before writing an exclusion.
- Preserve endpoint-qualified removal and early removal of undiscoverable
  authored selections.
- Strengthen the ambiguity regression to prove the exact exclusion, removed
  organization output, preserved sibling/repository output, and unchanged
  repository configuration.
- Give optional organization collection refusal a stable diagnostic identity,
  without changing required-content refusal.

## Capabilities

### Modified Capabilities

- `resource-removal`: catalog-selected resources participate in organization
  opt-out ambiguity checks; optional collection refusal has a stable identity.
