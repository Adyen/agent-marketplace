# Resource identity, revisions, and installed names

`agent-marketplace` separates the identity of a resource from the name it receives in a consumer's harness layout.

## Resource identity

A resource is identified by:

- **Marketplace coordinate**: `marketplace/registry/kind/name`.
- **Content digest**: a SHA-256 digest of the canonical resource contents.
- **Revision**: an immutable content-addressed revision, often the same as the content digest.
- **Archive digest**: a SHA-256 digest of the published artifact bytes.

## Installed name

The `installed_name` is the local name in the consumer's harness layout. It defaults to the Marketplace `name`. Consumers can override the default by supplying a different installed name at install time; the Marketplace coordinate stays the same while the generated files use the chosen name.

See [How to install a resource from a hosted Marketplace](../how-to/install-from-a-marketplace.md).

## Revisions

Each revision is independent. Updating a resource does not change its old revision. Consumers pin exact revisions so a `sync` always reproduces the same bytes.

## Identity and names in Git registries

For Git registries, the identity is the `(registry, kind, name)` tuple. The installed name is the resource name in the native layout. Name collisions across registries are resolved deterministically with a warning.

## Related documentation

- [Marketplace resource locators reference](../reference/marketplace-resource-locators.md)
- [How to install a resource from a hosted Marketplace](../how-to/install-from-a-marketplace.md)
