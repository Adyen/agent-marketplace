## Why

A `catalog.requires.invalid` warning on a loaded resource is currently treated as evidence that an organizational sub-bundle was ignored. The warning is about malformed advisory metadata, not resource discovery; misclassifying it prints a false `catalog.discover.sub-bundles-ignored` diagnostic on an otherwise fully discovered registry.

## What Changes

- Keep malformed prerequisite warnings visible, but do not treat them as evidence that a sub-bundle contains missed resources.
- Add a regression test exercising a covered organizational sub-bundle with an invalid `metadata.requires` item and a valid sibling. Leave other discovery-boundary warnings unchanged.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `resource-prerequisites`: when a malformed prerequisite occurs in a discovered resource, keep that resource and sibling grouping while reporting only the relevant coded prerequisite warning.

## Impact

`internal/catalog` discovery warning classification and its tests, plus the `resource-prerequisites` OpenSpec requirement. This is an in-scope correction of issue #401's diagnostic path, with no changes to selection or the hosted Marketplace.
