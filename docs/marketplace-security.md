# Marketplace security

## Explicit limitations

- The Marketplace API and Nuxt UI do not implement authentication, sessions, tenants, or per-resource authorization.
- `/healthz` is intentionally unauthenticated and only reports process liveness.
- `/readyz` and `/metrics` are operations endpoints, not an authorization mechanism; they must remain on a private network. The public listener and bundled proxy return explicit 404s for these paths.
- The worker executes `git` and accesses configured registry origins. It is not a general-purpose sandbox and must have restricted egress and a dedicated runtime identity.
- Storage credentials and registry credentials are references, not a secret-management implementation. Rotation, access policy, encryption, and audit logging belong to the deployment environment.

## Required boundary

Do not expose the API or UI directly to an untrusted network. Place a TLS-terminating reverse proxy, identity-aware gateway, or private service mesh in front of the proxy port. Enforce authentication, authorization, rate limits, request-size limits, and allowed methods there. The application currently serves read endpoints without an auth challenge, so a network boundary is mandatory for any sensitive deployment.

Do not expose the operations listener publicly. Bind it to loopback or a private interface, do not publish its container port, and allow only a monitoring plane to reach it. The Compose examples route only `/api/`, `/.well-known/agent-marketplace.json`, and `/healthz` to the API; `/readyz` and `/metrics` are explicit public 404s by design.

## Secrets and logs

Prefer `dsn_env` supplied by the process environment or `dsn_file` mounted by a secret manager. Configure exactly one DSN source. Keep secret files outside the repository with restrictive permissions. Do not use literal DSNs in production, and do not put secrets in URLs copied into logs or diagnostics.

Configuration resolution validates DSN source selection without including secret values in errors. Operators should still configure their logging and platform log redaction policies, especially for infrastructure, proxy, database, and cloud-provider logs.

A registry-specific `ca_file` is trust configuration rather than a credential. Mount each bundle read-only and scope it only to registries that require that authority. Marketplace acquisition does not inherit ambient `GIT_SSL_CAINFO`; it sets that variable from the registry configuration without changing process-global or user Git settings. Never disable TLS verification to accommodate an internal CA.

License policy fails closed. With `policy.license_allowlist` configured, a resource that declares a license outside it is rejected as `marketplace.policy.license-not-allowed`, and a resource that declares no license at all is rejected as `marketplace.policy.license-undeclared` unless `policy.license_undeclared` is explicitly set to `warn` or `allow`; the default, an unset value, and an unrecognised per-registry override all mean `reject`. Setting `warn` or `allow` mirrors and serves content whose redistribution terms this instance does not know, which is a legal and licensing decision rather than a defect to be worked around. See the [operator guide](marketplace-operator.md) for the full option.

Repository-local relative symlinks are dereferenced in the isolated checkout before ingestion. Absolute links, repository escapes, broken links, cycles, special files, and links into `.git` are rejected. Expanded files count against repository size, per-file size, and file-count limits, and published artifacts remain symlink-free.

## Artifact integrity

V2 separates SHA-256 payload identity, framed decoded-entry identity, and raw compressed-blob identity. The blob route resolves current resource ownership and restrictions before storage access, then verifies the complete reference and resource identity. Unowned, revoked, and blocked references cannot expose bytes. Blob addressing is not authentication or authorization by itself and does not replace the network boundary above.

Exact v1 and v2 locks cannot silently switch schema. The frozen v1 codec continues authenticating the complete legacy archive. Backfill and writable lock upgrades require equivalence of every payload, asset, license, and retained manifest value; matching content digests alone is insufficient. V2 cache evidence binds schema, artifact digest, blob digest, and media type, and cache reads verify bytes again. Failed verification cannot mark evidence, publish a lock, or materialize a candidate.

## Isolation and updates

Run API and worker as non-root users. Give the worker only the writable directories it needs, use a read-only configuration mount, and use separate service identities where possible. Keep the worker's outbound access limited to approved Git and object-storage destinations. Keep database and blob stores on private networks.

Pin image tags according to the organization's update policy, scan images and dependencies, and rebuild for security updates. Container static checks do not replace image scanning or runtime policy enforcement.
