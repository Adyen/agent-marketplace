## Approach

Reuse the v4 layer editor and staged removal/sync flow to add a Marketplace
source-scoped exclusion. Bind the destination source by canonical endpoint, not
by the organization's local Marketplace alias. Build the exclusion from the
verified registry slug, resource kind and canonical resource name.

This applies to optional direct organization selections and collection members.
It does not remove collection installation selectors or change organization
files. Required policy and machine-scope validation run before editing.

Native-hook removal preflight shares transient organization acquisition evidence
between its before/after resolutions. This validates the staged user exclusion
without attempting fresh acquisition against an unpublished configuration overlay.

## Alternatives

- Merely improve the refusal: leaves an existing resolver-supported workflow
  inaccessible from the CLI.
- Introduce whole-collection exclusions: changes selection and acquisition
  semantics unnecessarily; per-member opt-out solves the reported defect.

## Verification

Prove CLI removal with absent organization acquisition locks, default and explicit
user scope, endpoint qualification, and multiple resource kinds. Assert member
pruning, sibling preservation, unchanged organization bytes, required refusals,
repo/local boundary refusals, and no staging residue.
