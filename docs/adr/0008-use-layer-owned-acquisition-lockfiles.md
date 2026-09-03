---
status: accepted
---

# Use layer-owned acquisition lockfiles

Agent Marketplace records remote acquisition resolutions in one generated lockfile owned by each user, repository, or local configuration layer, and optionally by the organization layer, then composes locked catalogs before applying effective selection. An organization without a lock remains deliberately unlocked and consumer commands never create that lock implicitly. This avoids a machine-specific effective lock and keeps human intent separate from generated resolution while still allowing normal sync to minimally reconcile config changes and explicit update to advance compatible resolutions; plain filesystem directory registries remain deliberately live and unlocked. Hostless `file://` URLs ending in `.git` are supported remote Git registries and can be locked.

## Considered options

A single effective-environment lock was rejected because it would mix organization, user, repo, and personal-local state in one nonportable committed snapshot. Inline resolutions plus a checksum ledger were rejected because generated resolution churn would remain mixed into hand-authored policy and Marketplace configuration.

## Consequences

This ADR supersedes only the storage-location consequences of ADRs 0002 and 0005: Marketplace identity and content-addressed collection semantics remain accepted, while exact Marketplace revisions, collection graphs, and selection edges move from consumer configuration into the owning layer's lockfile.
