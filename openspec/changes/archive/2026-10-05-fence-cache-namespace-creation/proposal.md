## Why

Cache parent preparation and producer directory creation reopen paths before captured support copy. Retargeting the selected parent at that boundary creates a compiled cache in a foreign directory.

## What Changes

- Admit/create the selected cache parent through the existing captured machine-directory creator, preserving initial aliases.
- Reuse its descriptor and existing named-root boundary checks before producer and compiled-cache preparation.
- Create private producer directories through Mkdir below that descriptor with standard-library cryptographically random names, avoiding file-reservation unlink races.
- Retain existing admitted producer inode cleanup and diagnostic flow.

## Capabilities

### Modified Capabilities

- `materialization`: cache namespace creation authority.

## Impact

R2 child of #537 in stack #478. No new public API/type/helper/dependency/processing budget/source comments. Reuse harness/fsboundary/destinationBoundary and Go1.26 crypto/rand.Text. Selected initial parent aliases retain relocation; late retargets refuse. Cache compilation, support copy and native publication are not one transaction; no atomic uncooperative-writer exclusion. Existing SDK publication and final admitted-case audit remain separately evaluated.
