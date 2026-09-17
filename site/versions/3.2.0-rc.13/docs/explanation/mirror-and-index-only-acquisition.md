# Mirror and index-only acquisition

A hosted Marketplace can publish resources in two acquisition modes. The mode affects where the consumer gets the bytes and who hosts the artifact.

## Mirror

In `mirror` mode, the Marketplace stores and serves the artifact. The consumer downloads the artifact from the Marketplace and verifies it by content digest.

This mode is best for:

- Speed, because the Marketplace can serve content from a CDN.
- Availability, because the consumer does not need to reach the registry origin.
- Audit, because the Marketplace controls the served artifact.

## Index-only

In `index-only` mode, the Marketplace publishes verified metadata but the consumer fetches the exact source occurrence from the registry origin. The Marketplace still records the commit and manifest digest for verification.

This mode is best for:

- Registries that cannot be mirrored due to license or policy.
- Consumers who want to keep a direct relationship with the registry.
- Reducing Marketplace storage.

## Configuration

The operator chooses the instance-wide default with `acquisition_default` and can override it per approved registry with `acquisition`. These fields only control how the Marketplace publishes a registry's resources; they do not change the consumer-side pin format. See the [Marketplace operator configuration reference](../reference/marketplace-operator-configuration.md) for the exact schema.

## Consumer-visible difference

The consumer CLI installs both modes through the same locators. `index-only` pins include a `registry_commit` and `registry_manifest_digest` instead of a mirror archive digest.

## Related documentation

- [Marketplace operator configuration reference](../reference/marketplace-operator-configuration.md)
- [Marketplace resource locators reference](../reference/marketplace-resource-locators.md)
