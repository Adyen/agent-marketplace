## Why

Native collection discovery reads COLLECTION.yaml by pathname, independently of the guarded primary catalog reader. A foreign linked definition can contribute ownership/selection input, or replace the installed collection's members during sync.

## What Changes

- Capture collection discovery's registry root and use its directory and strict regular/single-link/nonblocking payload snapshots.
- Preserve relative contained file aliases, provenance, deterministic listing, optional genuine absence and established flat-file/subdirectory filtering.
- Retain typed source refusal rather than treating unsafe dangling metadata as absence; native sync must preserve installed/external generations.

## Capabilities

### Modified Capabilities

- `registry-symlink-containment`: native collection definition read authority.

## Impact

R2 child of #480. No new absolute byte/tree/work budget, whole-source atomic snapshot, or claims about portable output/prune, aggregate, restore or ownership consumers.
