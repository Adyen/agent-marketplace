## Why

Removing a member of an optional organization Marketplace collection tells the
user to remove the collection, but removing that collection is forbidden.
The resolver already honors exact user exclusions; the CLI does not write them.

## What Changes

- Named removal of optional organization Marketplace resources writes an exact
  exclusion in the user configuration, including collection-selected members.
- Omitted scope defaults to user; explicit repo/local scope retains the existing
  machine-scope boundary refusal.
- Required content remains protected. Organization collection removal explains
  that optional members can be excluded individually.
- Organization configuration and acquisition locks remain externally managed;
  collection-level Marketplace exclusions remain unsupported.

## Capabilities

### Modified Capabilities

- `resource-removal`: user opt-out of optional organization Marketplace resources.

## Impact

The shared removal pipeline and v4 layer editor gain an exact Marketplace
exclusion edit. No consumer or acquisition-lock format changes.
