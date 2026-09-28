# Marketplace trust model

When you install from a hosted [Marketplace](../glossary.md#marketplace), you rely on several parties: the people who wrote the resource, the operator who approved its registry, the Marketplace server, the infrastructure it runs on, and the `agent-marketplace` CLI on your machine. This page explains what each one checks and what it leaves to the others.

## What the server relies on

The Marketplace server treats these inputs as authoritative:

- The list of approved registries. It comes from the operator configuration file, or from the metadata database after an operator adopts database management.
- The Git origin and ref of each approved registry.
- The credentials and CA bundles the operator references for private registries. The configuration holds only file paths or environment variable names, never the secret values.
- The content digests that its own worker computes during ingestion.
- The revocations the operator declares.

## What the server checks

Before a revision is published, the worker runs security analyzers over the resource and its files, and applies the operator's license, size, and hidden-content policy. The analyzers look for things such as hidden HTML comments, invisible or bidirectional Unicode characters, terminal control sequences, dangerous or remote URLs, executable files and scripts, dynamic code execution, tool grants, and hook or MCP capabilities. Each finding has an outcome of `reject`, `warn`, or `allow`, and together they form the revision's assessment.

The server stores and lists a rejected revision so people can see it, but it does not serve the revision's artifact. An operator can override that per instance or per registry with `rejected_resources: expose`. The revision then appears as `operator-exposed` and keeps its reject assessment.

The operator can revoke a whole registry, every revision of one resource, or one exact revision. A revoked revision is never served, and no policy setting can override that.

Before it returns an artifact or a manifest, the server checks that the stored bytes match the digest in the request. A digest that no published revision owns returns `404`, even if bytes exist at that address.

## What the server does not do

- It does not authenticate or authorize callers of the public catalog. Anyone who can reach the public listener can search and download.
- It does not decide per person who may install which resource.
- It does not terminate TLS. Both listeners speak plain HTTP.
- It does not encrypt data at rest.
- It does not store, rotate, or distribute secrets. It reads them from the files or environment variables the operator references.

The optional admin panel is the one place with built-in authentication. When enabled, it signs users in through OIDC or accepts identity headers from a trusted proxy, and it admits only configured admin groups or users.

## Operator responsibilities

The deployment environment covers what the server does not. The operator is responsible for:

- a reverse proxy that terminates TLS and, if the catalog is not meant for everyone who can reach it, enforces identity and authorization in front of the API and UI;
- keeping the operations listener (`/readyz` and `/metrics`) on a private network;
- limiting the worker's outbound network access to approved Git and object-storage destinations;
- running containers as non-root users;
- keeping database connection strings and registry credentials in a secret manager, referenced from the configuration; and
- setting license and content policy explicitly.

These are infrastructure and policy decisions. The CLI does not make them for you.

## What the CLI checks

On your machine, `agent-marketplace` adds its own checks:

- When you register an endpoint, it reads the well-known document and records the endpoint's `instance_id`. Later commands compare it again, so a URL that starts pointing at a different deployment is refused.
- An `allowed_marketplaces` policy in your configuration can limit which endpoints the CLI talks to at all.
- It installs exact revisions and checks the digests of what it downloads against the revision it verified.
- Before it installs a revision with security findings, it shows them and asks you to confirm. In automation, `--accept-risk` or `--yes` gives that consent for the exact revision verified in that run. Capabilities are shown but do not require consent on their own.
- A revision the Marketplace rejected cannot be accepted with `--accept-risk`, unless the operator exposed it.
- A revoked revision is blocked, and `sync --check` reports it. `sync --allow-revoked` allows one exact revoked revision for a single sync, and never makes `sync --check` pass.
- `sync --offline` uses cached Marketplace data, so it cannot see revocations published after the cache was filled.

## Related documentation

- [Marketplace security](https://github.com/Adyen/agent-marketplace/blob/main/docs/marketplace-security.md)
- [How to deploy a production Marketplace](../how-to/operator/deploy-a-production-marketplace.md)
- [Marketplace architecture](./marketplace-architecture.md)
