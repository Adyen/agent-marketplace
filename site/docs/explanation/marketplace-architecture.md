# Marketplace architecture

A hosted [Marketplace](../glossary.md#marketplace) is a web service that a team runs so its members can search for and install agent resources from one place. It reads approved [Git](../glossary.md#git) registries, checks and stores their content, and answers the `search` and `install` requests that `agent-marketplace` sends. You never need to run one to use a Marketplace; this page explains what sits behind the URL you install from.

## The pieces

A deployment has a few parts, each with one job:

- The worker clones each approved registry, reads the resources in it, runs the security analyzers and license and size policy, and stores the results. In `database` management mode it also runs discovery previews that the admin panel requests.
- The API answers HTTP requests: the well-known identity document, search, resource and revision metadata, assessments, inspection content, artifacts, health, and the optional admin routes.
- The Nuxt UI serves the Marketplace pages you see in a browser, including the admin panel.
- The VitePress site serves the static landing page and these docs.
- A reverse proxy in front of everything terminates TLS and routes each request to the API, the UI, or the static site.

The worker and the API are the same `agent-marketplace serve` program. The `--role` flag runs one of them or both in one process.

## From registry to your harness

1. The operator lists the approved registries. With `management.mode: file` they are in the operator configuration file; with `database` mode an administrator manages them in the admin panel.
2. The worker clones each registry at its configured ref, on a schedule (every 30 minutes by default) or when an operator runs `serve reindex`.
3. The worker reads the resources, runs the analyzers and policy checks, computes a content digest for each resource, and records the result as an assessment. In `mirror` mode it also stores an artifact, a compressed archive of the resource, under its digest.
4. The API publishes the catalog. Each revision is identified by its content digest, and its content stays unchanged after publication. A repaired archive can update the served archive digest without changing that identity; the previous artifact remains downloadable.
5. You run `agent-marketplace search` and `install`. The CLI verifies the revision, records its exact digest so later syncs install the same content, and writes the files into your [harness](../glossary.md#harness) folders.

## Storage

The server keeps two kinds of data:

- Metadata, such as registries, revisions, assessments, and revocations, lives in SQLite or PostgreSQL.
- Blobs, meaning artifacts, manifests, and inspection files, live on a filesystem or in S3. Each blob is stored under its SHA-256 digest.

Back up metadata and blobs together. If you restore one without the other, the metadata can point to blobs that do not exist, or history can be incomplete.

## Operations listener

The server opens a second, private listener for `/readyz` (is the server ready to serve?) and `/metrics` (Prometheus data). Do not publish it through the reverse proxy. The public listener answers `404` for both paths.

## Authentication

The public catalog routes are anonymous: the server does not identify or authorize whoever calls them. If only certain people may browse or install, the reverse proxy must enforce that.

The admin routes are different. When the operator turns them on, they authenticate every request, either through native OIDC sign-in with a signed session cookie or through identity headers set by a trusted reverse proxy. Access requires a configured admin group or user.

## Related documentation

- [How to deploy a production Marketplace](../how-to/operator/deploy-a-production-marketplace.md)
- [How to configure a reverse proxy](../how-to/operator/configure-a-reverse-proxy.md)
- [Marketplace trust model](./marketplace-trust-model.md)
