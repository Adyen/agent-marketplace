## Context

The CLI rejects multiple names and both configured and ad-hoc pipeline branches read only `Args[0]`. CLI installs already stage config edits and reconcile dependency locks through one prepared dependency view. Existing result rendering supports multiple installed names.

## Goals / Non-Goals

Goals are multiple named selections in one operation, complete preflight before publication, first-request-order deduplication, and compatibility with existing selections and output.

Non-goals are batch Marketplace acquisition, new flags, config schemas, TUI changes, and unrelated refactoring.

## Decisions

- Resolve names into a compact ordered selection plan containing registry entry and typed locator. Share planning and applying between configured and explicit-source branches rather than looping over `Install` calls. Collection selections use existing expansion and scope validation. Preserve the existing singleton collection result.
- Restrict `--from` name resolution to its exact selected catalog. Configured names retain current provider precedence and kind disambiguation.
- Apply planned selections in one layer mutation, clear explicit-resource exclusions through the existing staging manager, then call the existing sync path once with the union of requested identities and collection members for warning filtering.
- Normalize and deduplicate names before selection. Validate all batch names before selection edits. CLI staged publication must preserve absent config roots on failure. Avoid extending direct-pipeline legacy publication behavior outside the batch contract.
- Reject batched Marketplace locators/endpoints and Marketplace-only flags during argument validation, examining every argument rather than just the first.
- Keep no-name `--from`, `--all`, and `--tag` branches intact. Do not add a new result schema or batching subsystem.

## Risks / Trade-offs

Collection expansion and exclusions must be validated before any batch selection is published. Tests must exercise the actual command transaction boundary, missing roots, scoped configs, unrelated locks and output, not only helper results. Cache acquisition is permitted during preflight. The scope and provider checks must remain shared with singleton behavior.
