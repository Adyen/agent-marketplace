# Marketplace security

## Explicit limitations

- The Marketplace API and Nuxt UI do not implement authentication, sessions, tenants, or per-resource authorization.
- `/healthz` is intentionally unauthenticated and only reports process liveness.
- `/readyz` and `/metrics` are operations endpoints, not an authorization mechanism; they must remain on a private network. The public listener and bundled proxy return explicit 404s for these paths.
- The worker executes `git` and accesses configured source origins. It is not a general-purpose sandbox and must have restricted egress and a dedicated runtime identity.
- Storage credentials and source credentials are references, not a secret-management implementation. Rotation, access policy, encryption, and audit logging belong to the deployment environment.

## Required boundary

Do not expose the API or UI directly to an untrusted network. Place a TLS-terminating reverse proxy, identity-aware gateway, or private service mesh in front of the proxy port. Enforce authentication, authorization, rate limits, request-size limits, and allowed methods there. The application currently serves read endpoints without an auth challenge, so a network boundary is mandatory for any sensitive deployment.

Do not expose the operations listener publicly. Bind it to loopback or a private interface, do not publish its container port, and allow only a monitoring plane to reach it. The Compose examples route only `/api/`, `/.well-known/agent-marketplace.json`, and `/healthz` to the API; `/readyz` and `/metrics` are explicit public 404s by design.

## Secrets and logs

Prefer `dsn_env` supplied by the process environment or `dsn_file` mounted by a secret manager. Configure exactly one source. Keep secret files outside the repository with restrictive permissions. Do not use literal DSNs in production, and do not put secrets in URLs copied into logs or diagnostics.

Configuration resolution validates source selection without including secret values in errors. Operators should still configure their logging and platform log redaction policies, especially for infrastructure, proxy, database, and cloud-provider logs.

## Isolation and updates

Run API and worker as non-root users. Give the worker only the writable directories it needs, use a read-only configuration mount, and use separate service identities where possible. Keep the worker's outbound access limited to approved Git and object-storage destinations. Keep database and blob stores on private networks.

Pin image tags according to the organization's update policy, scan images and dependencies, and rebuild for security updates. Container static checks do not replace image scanning or runtime policy enforcement.
