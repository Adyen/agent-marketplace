## Why

Consumer how-to guides need reliable steps for readers who already know their task. Several examples lack expected results or mix practical directions with reference detail.

## What Changes

- Audit every consumer guide directly under `site/docs/how-to/` for its task, reader, prerequisites, actions, verification, and recovery.
- Check published commands, configuration, paths, diagnostics, and output against current source and governing specifications.
- Tighten prose and supply exact or labelled typical results for command examples.
- Keep existing URLs by default and restrict changes outside the section to necessary link fixes.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. This documentation audit changes no specified behavior and sets `skip_specs: true`.

## Impact

Consumer how-to Markdown and, only if needed, site navigation and inbound links. No runtime, test, normative specification, release snapshot, author guide, or operator guide changes.
