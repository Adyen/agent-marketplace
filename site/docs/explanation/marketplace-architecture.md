# Marketplace architecture

A hosted Marketplace is a service that indexes Git registries, stores immutable artifacts, and serves a catalog to consumer installations.

## Roles

- **API**: serves read-only catalog, artifact, well-known, and public health routes.
- **Worker**: clones registries, ingests resources, and stores artifacts.
- **Nuxt UI**: serves the Marketplace browser pages.
- **VitePress site**: serves the static landing and documentation pages.
- **Reverse proxy**: terminates TLS and routes to the API, UI, or static site.

## Data flow

1. The operator configures approved registries in `marketplace.yaml`.
2. The worker clones each registry at the configured ref.
3. The worker ingests resources, applies policy, computes content digests, and stores artifacts.
4. The API serves the catalog and immutable artifacts.
5. The consumer CLI searches the API and installs exact pins.

## Persistence

- **Metadata**: SQLite or PostgreSQL.
- **Blobs**: filesystem or S3.

Back up metadata and blobs together. Restoring one without the other can return incomplete history.

## Operations listener

The operations listener exposes `/readyz` and `/metrics` on a separate, private address. It must not be published through the reverse proxy.

## Stateless API

The API does not implement authentication or sessions. The reverse proxy must enforce identity and authorization.

## Related documentation

- [How to deploy a production Marketplace](../how-to/operator/deploy-a-production-marketplace.md)
- [How to configure a reverse proxy](../how-to/operator/configure-a-reverse-proxy.md)
- [Marketplace trust model](./marketplace-trust-model.md)
