# Marketplace security

## Explicit limitations

- The public Marketplace API and the Nuxt UI have no authentication, tenants, or per-resource authorization. Only the optional admin API authenticates callers, through a native OIDC session or a trusted proxy's identity headers.
- `/healthz` is unauthenticated by design and reports only process liveness.
- `/readyz` and `/metrics` are operations endpoints and provide no authorization, so they must stay on a private network. The public listener and the bundled proxy answer both paths with an explicit 404.
- The worker runs `git` and connects to the configured registry origins. It is not a general-purpose sandbox, so give it restricted egress and a dedicated runtime identity.
- Storage and registry credentials are references to secrets; the application does not manage secrets itself. Rotation, access policy, encryption, and audit logging belong to the deployment environment.

## Required boundary

Do not expose the API or UI directly to an untrusted network. Put a TLS-terminating reverse proxy, identity-aware gateway, or private service mesh in front of the proxy port, and enforce authentication, authorization, rate limits, request-size limits, and allowed methods there. The application serves its read endpoints without an authentication challenge, so any sensitive deployment needs this network boundary.

Do not expose the operations listener publicly. Bind it to loopback or a private interface, do not publish its container port, and let only a monitoring plane reach it. The Compose examples route only `/api/`, `/.well-known/agent-marketplace.json`, and `/healthz` to the API, and answer public requests for `/readyz` and `/metrics` with 404.

## Secrets and logs

Supply the metadata DSN through `dsn_file`, mounted by a secret manager, or through `dsn_env`, set in the process environment from a platform secret. Configure exactly one DSN source. Keep secret files outside the repository with restrictive permissions. Do not use a literal `dsn` in production, and do not put secrets in URLs that may be copied into logs or diagnostics. Registry `origin` values must be HTTP(S) URLs without userinfo, fragments, or credential query parameters; configuration validation rejects the rest.

Configuration validation checks which DSN source is selected without including secret values in its errors. Operators still need their own log redaction policies, especially for infrastructure, proxy, database, and cloud-provider logs.

A registry-specific `ca_file` is trust configuration, not a credential. Mount each bundle read-only and configure it only on the registries that need that authority. Marketplace acquisition does not inherit an ambient `GIT_SSL_CAINFO`. It removes the variable from the Git environment and sets it from the registry's `ca_file` when one is configured, without changing process-wide or user Git settings. Never disable TLS verification to accommodate an internal CA.

License policy fails closed, meaning that without an explicit choice it rejects the resource. With `policy.license_allowlist` configured, a resource that declares a license outside the list is rejected as `marketplace.policy.license-not-allowed`. A resource that declares no license at all is rejected as `marketplace.policy.license-undeclared` unless `policy.license_undeclared` is set to `warn` or `allow`. The default, and an unset value, both mean `reject`. A per-registry `policy_overrides.license_undeclared` with an unrecognized value is ignored with a warning, and the global value applies. Setting `warn` or `allow` mirrors and serves content whose redistribution terms this instance does not know, which is a legal and licensing decision for the operator. See the [operator guide](marketplace-operator.md#license-policy) for the full option.

Repository-local relative symlinks are dereferenced in the isolated checkout before the content is read. Absolute links, links that escape the repository, broken links, cycles, special files, and links into `.git` are rejected. Expanded files count against the repository size, per-file size, and file-count limits, and published artifacts contain no symlinks.

## Isolation and updates

Run the API and worker as non-root users. Give the worker only the writable directories it needs, mount the configuration read-only, and use separate service identities where possible. Limit the worker's outbound access to approved Git and object-storage destinations. Keep the database and blob stores on private networks.

Pin image tags according to the organization's update policy, scan images and dependencies, and rebuild for security updates. The repository's container static checks do not replace image scanning or runtime policy enforcement.
