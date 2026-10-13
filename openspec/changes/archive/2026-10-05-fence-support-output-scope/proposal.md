## Why

Support source reads were captured, but destination directories and producer files were created through reopened paths. A producer-root retarget before traversal can create foreign directories and write source bytes into them before cleanup refuses.

## What Changes

- Capture support output once, lazily at the first eligible entry.
- Reuse the existing private destination boundary with that captured root, checking named-root identity on each directory capture.
- Create descendant directories and producer files descriptor-relatively; retain the same boundary for snapshot reads/publication.
- Preserve empty-source/excluded-source no-op and existing source modes/contained input aliases.

## Capabilities

### Modified Capabilities

- `materialization`: captured support output traversal/preparation.

## Impact

R2 child of #536 in stack #478. No public API, dependencies, processing budgets or source-comment changes. Reuse fsboundary and existing publisher/boundary; private support writer signatures carry that boundary. This does not make cache/native publication transactional or assert atomic exclusion of uncooperative writers. Initial producer/cache namespace creation before copy admission remains subject to final case audit.
