## Why

The public reference pages mix lookup facts with instructions and explanation. Before launch, they need a factual audit against the governing specifications and current source, with concise prose for practitioners.

## What Changes

- Audit all 19 pages under `site/docs/reference/` for their reference quadrant and reader.
- Correct documented commands, flags, configuration, paths, variables, diagnostics, defaults, outputs, and failure cases without changing runtime behavior.
- Remove procedural and explanatory prose in favor of links to existing guides.
- Correct navigation and inbound links only when the reference audit requires it.
- Verify the site build, documentation contract, and repository gates, retaining safe command captures outside the repository.

## Capabilities

### New Capabilities

None. This documentation-only change sets `skip_specs: true`.

### Modified Capabilities

None. Existing normative requirements remain unchanged.

## Impact

Public reference pages and any necessary site link or navigation fixes. No runtime code, tests, API schemas, dependencies, or normative specifications change.
