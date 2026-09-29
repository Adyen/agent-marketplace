## Why

The `marketplace-protocols` → `marketplace-api` rebaseline left consumer Marketplace configuration in a protocol capability: the *Consumer Marketplace configuration* requirement describes Marketplace endpoint registrations, Marketplace installations, and the `allowed_marketplaces` allowlist, and its merge scenario states layered-merge rules under the drift-corrected title "Layered config wiring", which no longer matches its content. Consumer configuration belongs to a configuration-layering capability, and the harvest ledger rows mapping `docs/PRECEDENCE.md` and `docs/plans/2026-09-07-unified-resource-config-design.md` to `config-layering` are still unabsorbed.

## What Changes

- Adds a `config-layering` capability specifying how consumer Marketplace configuration is expressed in v4 and in v2/v3 roots, how entries are validated, and how they merge across the organization, user, repository, and personal-local layers.
- Removes the `marketplace-api` requirement *Consumer Marketplace configuration* with a migration note pointing at `config-layering`, and drops the capability Purpose's consumer-configuration phrase.
- Retitles the moved layered-merge scenario so its title matches its content, and moves every `// spec: marketplace-api/...` consumer-configuration annotation to the new `config-layering/` identities.
- Adds scenarios for observable consumer-configuration behaviors already proven by existing passing tests but not yet bound to a scenario: v4 and v2/v3 expression, duplicate and reserved alias rejection, layered alias acceptance for one endpoint identity, unsafe identity and endpoint field rejection, personal-local load validation, and v4 include-tree allowlist authority. Each is proven by adding one `// spec:` annotation line to those tests; no new test logic.
- Absorbs harvest rows 1.1 and 4.14: `docs/PRECEDENCE.md` and `docs/plans/2026-09-07-unified-resource-config-design.md` are deleted; every claim from them that is not carried into the spec is listed under **Discarded claims** in `design.md`; every remaining reference to either document is removed or repointed.
- **No behavior changes**: no runtime code or test-logic edits; only spec prose, annotation comment lines, the harvest ledger, dead-link fixes, and deleted legacy documents.

## Capabilities

### New Capabilities

- `config-layering`: consumer configuration layers — how Marketplace endpoint registrations, Marketplace installations, and the `allowed_marketplaces` allowlist are expressed, validated, and merged across the organization, user, repository, and personal-local layers.

### Modified Capabilities

- `marketplace-api`: the *Consumer Marketplace configuration* requirement is removed; the capability keeps its protocol-surface requirements.

## Impact

- Documentation and specs only; no runtime behavior changes.
- Test files in `internal/config` change in comment lines only (three annotations move; up to ten annotations are added on existing passing tests).
- `openspec/changes/harvest-legacy-docs/tasks.md` checks off rows 1.1 and 4.14.
- `README.md`, `AGENTS.md`, `docs/adoption-guide.md`, `docs/tui-product-contract.md`, and `docs/plans/2026-09-24-pre-v4-regression-gates.md` lose their references to the deleted documents.
