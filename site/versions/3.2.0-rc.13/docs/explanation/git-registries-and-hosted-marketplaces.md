# Git registries and hosted Marketplaces

`agent-marketplace` can acquire resources from plain Git registries and from a hosted Marketplace. Each has different trust and reproducibility properties.

## Git registries

A Git registry is a Git repository that contains resources in a supported harness layout. The consumer points at the URL, ref, and path. The CLI resolves the ref to an exact commit, writes a lock, and materializes the resources.

Git registries are self-hosted and versioned. They work without any server beyond Git.

## Hosted Marketplaces

A hosted Marketplace is an HTTP service that indexes registries, publishes immutable artifacts, and can enforce ingestion policy. Consumers search the catalog and install exact revisions by digest.

A Marketplace endpoint is identified by:

- A canonical URL.
- An `instance_id` verified through `/.well-known/agent-marketplace.json`.
- An optional `alias` for shorter locators.

## Locators

- Compact: `<alias>:<registry>/<kind>/<name>`
- URL: `https://marketplace.example.com/r/<registry>/<kind>/<name>`
- Compact exact: `<alias>:<registry>/<kind>/<name>@sha256:<64-hex>`
- URL exact: `https://marketplace.example.com/r/<registry>/<kind>/<name>/revisions/sha256:<64-hex>`

## Acquisition modes

- `mirror`: the Marketplace hosts the artifact.
- `index-only`: the consumer fetches the exact source occurrence from the registry origin.

## Trust differences

- Git registries trust the Git transport and the committed lock.
- Hosted Marketplaces add operator policy, license allowlists, and the operator's chosen revocations.

## Mixed use

A consumer can use both at the same time. Git registries appear under `registries` and use lockfiles; Marketplace pins appear under `marketplaces` and use exact digest records.

## Related documentation

- [How to install a resource from a hosted Marketplace](../how-to/install-from-a-marketplace.md)
- [Marketplace resource locators reference](../reference/marketplace-resource-locators.md)
