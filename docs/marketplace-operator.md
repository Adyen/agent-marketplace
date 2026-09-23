# Marketplace operator guide

## Deployment shape

Run the static Go binary as two roles when possible:

- `serve --role api` serves read-only catalog, artifact, well-known, and public health routes. Browser pages are served only by the separate Nuxt frontend; `/r/**` returns 404 from this API-only role.
- `serve --role worker` performs repository acquisition, analysis, and indexing. The worker image includes `git`, CA certificates, and `/bin/sh` because acquisition runs there.
- `serve --role all` is suitable for a single-process development deployment, not the preferred production topology.

The API and worker can start concurrently. SQLite and PostgreSQL migration runners serialize schema changes and record applied versions. PostgreSQL uses a transaction-scoped advisory lock; SQLite uses an immediate transaction, WAL, busy timeout, and retry handling. A failed migration rolls back its transaction and should block startup rather than run with a partial schema.

Before moving API or consumer binaries from Go 1.26 to Go 1.27 or later, deploy this proof-capable release built with Go 1.26.6, stop the worker, and backfill the retained v1 artifact proofs. `serve backfill-artifact-proofs --config marketplace.yaml --limit 100` processes one bounded page; pass the returned `next_after` as `--after` until `remaining` is false. The command is resumable and validates existing proofs. It fails rather than publishing a proof if the active compressor cannot reproduce the exact existing archive bytes. Take a coordinated metadata/blob backup after completion, then upgrade API and consumer binaries. Keep workers that publish v1 artifacts on Go 1.26.6 until a compressor-independent publication protocol is deployed; otherwise unchanged logical content can produce a different `archive_digest` and conflict with immutable revision metadata.

## Network boundary

Public catalog APIs have no built-in user authentication or authorization. Put them behind a TLS-terminating, authenticated network boundary, or keep them on a private network. Restrict the proxy port with firewall/security-group policy. The optional administration API authorizes identities through exactly one configured mode: a signed session cookie issued by native OIDC Authorization Code + PKCE sign-in (`admin.authentication.mode: oidc`), or identity asserted by an explicitly trusted immediate proxy (`trusted_proxy`, inferred from the complete proxy field set). See `site/docs/how-to/operator/enable-and-use-the-admin-panel.md` for the operator contract.

`operations_listen` serves `/readyz` and `/metrics`. It defaults to loopback and must not be published through a container port or reverse proxy; the public listener returns 404 for both paths. If operators need metrics, collect them from a private sidecar/network path with its own access controls. `/healthz` is a liveness endpoint and does not prove storage readiness. Container API healthchecks use the private `/readyz` listener.

## Catalog projection consumers

The catalog projection endpoint is for internal consumers such as Backstage. It remains unauthenticated at the application layer.

The projection contains only resources from registries that this hosted Marketplace instance has approved and indexed. It does not include local registries, personal directories, direct Git sources, or ad-hoc CLI `--from` registries. Consumers must not treat CLI-discovered or locally configured registries as projection inputs.

Each current resource anchor is one of these six kinds: `skill`, `command`, `subagent`, `hook`, `mcpserver`, or `collection`. Blocked and revoked anchors remain in the projection with their availability state so a consumer can distinguish them from a resource that has disappeared; consumers must retain and reconcile those anchors rather than interpreting their absence or state as an instruction to silently discard identity.

Rows may include the sanitized source repository URL, repository-relative source path, and exact indexed commit. These fields can reveal repository structure and must not be exposed to a broader audience without confirming the gateway policy permits that disclosure.

### Scan and compatibility contract

Consumers read the projection as a stateless, bounded keyset scan. Start without a cursor, then use the opaque cursor from each response until the final page. Do not expect Marketplace to retain consumer cursors, snapshots, marks, sweep state, or any other per-consumer synchronization state.

The scan is moving rather than snapshot-consistent. Record the global ETag on the first and final pages. If they differ, schedule a catch-up scan: additions behind the cursor, updates, or removals may otherwise converge on the next cycle. Send `If-None-Match` only on the first page of a new scan, using the ETag from the last completed ingestion; continuation pages use only their cursor. Do not send conditional validators with continuation requests.

Before enabling a consumer, verify that it supports the endpoint's advertised API version and required catalog-projection capability. The endpoint is disabled by default. Enable it explicitly with:

```yaml
catalog_projection:
  enabled: true
```

When disabled, `/api/v1/catalog` is not routed and the catalog-projection feature is absent from both capability responses. Keep the consumer disabled until both its compatible version/capability check and this operator-configured gate are satisfied.

## Configuration and secrets

Set `instance.browser_base` to the public Nuxt base when browser pages are deployed, including its path prefix (for example, `https://marketplace.example/marketplace`); omit it for an API-only deployment. The well-known document then includes separate latest and exact browser route templates. Use `storage.metadata.dsn_file` in production, preferably through a Compose/platform secret integration. Exactly one DSN source must be configured and the selected value must be non-empty. Literal `dsn` and the environment-based `dsn_env` Compose fixture are for local development only. DSN values are not included in configuration errors or application startup logs.

Use file permissions and a secret manager for DSN files. For S3, prefer workload identity/IAM roles through the AWS SDK's default credential chain; if roles are unavailable, use an SDK-supported credentials file from the platform secret manager rather than static environment credentials. Do not put passwords in committed YAML, Compose files, image layers, command arguments, or issue output. Registry origins must not contain URL userinfo or password/token query parameters; put registry authentication in a credentials file or environment reference instead. Registry credential settings follow the same file/environment-reference rule. Production SQLite configuration must name a writable filesystem database; `:memory:`, `mode=memory`, `mode=ro`, and `immutable=1` URI modes are rejected (the SQLite package's direct in-memory constructor remains available to tests).

A registry that uses a private certificate authority may set `ca_file` to an absolute path for a readable, non-symlink PEM bundle containing at least one certificate. The worker copies the validated bundle to a private per-acquisition file and passes that copy as `GIT_SSL_CAINFO` only to Git subprocesses for that registry; it does not mutate global Git configuration or apply the bundle to other registries. Mount the file read-only at the same path in every API and worker container because all roles validate operator configuration during startup:

```yaml
registries:
  - id: internal-resources
    slug: internal-resources
    origin: https://git.example.internal/resources.git
    ref: main
    ca_file: /etc/agent-marketplace/ca/internal.pem
    roots:
      - path: .agents
```

When distinct registry paths publish the same kind and registry-local name, ingestion keeps the first occurrence in configured root order and logs `marketplace.ingest.duplicate-resource` with both paths and the remediation to assign distinct registry-local names. Repeating the same path remains an operator configuration error.

## Exercise database-managed administration in an isolated deployment

**Implementation status:** the managed backend can run after explicit adoption. The complete admin product is still experimental: acquisition previews, full resource diagnostics/impact analysis, and complete browser acceptance are not finished. Validate this procedure against a disposable copy of metadata and blobs first; this is not a production rollout approval.

### Prepare the deployment and identity boundary

1. Use the same managed-capable release for every API, worker, and maintenance process. Do not mix old workers with an adopted database.
2. Keep storage, listener addresses, instance identity, global non-license limits, and deployment revocations in operator YAML. Add `management.mode: database` and `management.allowed_origins`, listing exact allowed protocol/host/port authorities without repository paths. Database mode refuses to start before adoption.
3. If a registry uses credentials or a private CA, provision an entry in `management.profiles` with its `allowed_origins`, `credentials`, and optional `ca_file`. Adoption requires an unambiguous profile matching the registry's existing credential/trust references. Mount those references on every worker. Do not send secret values or filesystem paths to the browser.
4. Configure `admin.enabled`, an HTTPS `admin.origin` without a path, and `admin.admin_groups`/`admin.admin_users`, then select exactly one authentication mode. For `admin.authentication.mode: trusted_proxy` (inferred when the complete proxy field set is present without an explicit mode), also configure `admin.trusted_proxies` containing the immediate proxy CIDRs, `admin.issuer`, `admin.subject_header`, and `admin.groups_header`. The proxy must strip client-supplied identity headers and replace them after authentication. The subject header contains one stable subject; the groups header contains a JSON string array, such as `["marketplace-admins"]`, not a comma-separated list. Never trust a forwarded client IP as the immediate peer. For `admin.authentication.mode: oidc`, configure `admin.authentication.oidc` (`issuer`, `client_id`, exactly one of `session_secret_env`/`session_secret_file`, optional `groups_claim` and `session_ttl`) instead; the trusted-proxy identity fields are rejected in OIDC mode. See `site/docs/how-to/operator/enable-and-use-the-admin-panel.md` for the native sign-in flow and the Okta client contract.
5. In trusted-proxy mode, protect both `/api/v1/admin/**` and the browser's `/marketplace/admin/**` routes at the gateway, and prevent direct access around that gateway. In OIDC mode the gateway only routes admin traffic to the application and must not inject identity headers. Keep admin requests same-origin. Public SSR must not synthesize or forward an admin identity. Admin reads and writes are private and must not be shared-cacheable.

### Adopt during a maintenance window

1. Stop **all** API and worker processes. Take coordinated metadata and blob backups using the [backup guide](marketplace-backup-restore.md).
2. Invalidate controllable shared caches that may contain previously public immutable artifacts. Confirm the proxy will revalidate managed content through the API, including HEAD and conditional requests, and cannot bypass availability through direct object-store URLs. Previously downloaded content and browser caches cannot be recalled.
3. Retain every stored registry identity in the adoption YAML. Run the maintenance command with an operator identity, a reason, and a stable retry key:

   ```sh
   agent-marketplace serve adopt --config marketplace.yaml \
     --actor operator-id --reason 'Adopt catalog management' \
     --idempotency-key maintenance-cutover-1 --confirm-maintenance
   ```

   Expected output begins `Adopted marketplace configuration at revision 1.` Ownership backfill runs in bounded batches before the authority transaction. The acknowledgement flag does not independently prove shutdown, backup, or cache invalidation. If any step fails, keep processes stopped and resolve the failure; retry with the same request inputs and key rather than assuming adoption did not commit.

4. Start API and worker roles with `management.mode: database`. Confirm private `/readyz` succeeds, the authenticated configuration read returns the adopted revision, and an untrusted direct admin request is denied. Missing/corrupt authority state or missing runtime store capabilities must block startup. File mode cannot reopen an adopted database.
5. Verify an indexing attempt and a public resource read. In an isolated acceptance fixture, pause refresh and confirm existing content still serves, withdraw it and confirm downloads are denied, then restore availability and confirm the registry remains paused. A successful configuration request is not a successful publication.

### Operate after adoption

Database settings own registry configuration, lifecycle, and license policy. Restarting with stale registry YAML must not overwrite them. Remove obsolete registry entries from runtime YAML after preserving the adoption input separately; configuration loading still validates any entries left in that file. Keep deployment-owned profile definitions and non-license limits current.

`serve reindex --config marketplace.yaml` selects active managed registries, not the YAML registry list. A named registry is resolved against its managed ID or current slug and remains subject to worker lifecycle checks. Disabled/withdrawn registries are not implicitly resumed. Scheduled and manual work reads live settings and rechecks publication authority transactionally.

Use configuration activity and `/api/v1/admin/attempts` separately: an accepted change can have a failed or superseded attempt, and a failed attempt does not erase its previous successful publication. Attempt history accepts `registry_id`, `job_id`, a `before` cursor, and a limit of 1–100. It is not a worker heartbeat.

To remove access to administration, set `admin.enabled: false`; keep database mode. Disabling the interface does **not** restore YAML authority. Returning to file authority needs a separately reviewed export/cutover. Do not delete the adoption marker or restore metadata independently of its associated blobs.

## License policy

`policy.license_allowlist` lists the licenses this instance is willing to index and mirror. It defaults to `[MIT, Apache-2.0, BSD-2-Clause, BSD-3-Clause]`. An empty list disables license enforcement entirely: nothing is rejected or reported on license grounds, whatever a resource declares.

While an allowlist is configured, ingestion distinguishes two conditions, because they are not the same thing:

- **Disallowed** — the resource declares a license that the allowlist does not contain. The operator has seen that license and excluded it, so the resource is rejected, its intrinsic artifact is blocked from serving and installation, and it is reported as `marketplace.policy.license-not-allowed` naming the declared license.
- **Undeclared** — the resource declares no license at all. Nothing is known about its terms, which is not the same as knowing they are unacceptable, so the outcome is the operator's choice. It is reported as `marketplace.policy.license-undeclared`, never as `license-not-allowed`.

`policy.license_undeclared` selects that outcome and accepts `reject`, `warn`, or `allow`:

```yaml
policy:
  license_allowlist: [MIT, Apache-2.0]
  # reject (default) | warn | allow
  license_undeclared: reject
```

**The default is `reject`**, which is exactly the behaviour of releases that did not have this option: an undeclared license under a configured allowlist blocks the resource. `warn` keeps the resource installable and records a warn-severity finding, so operators can inventory unlicensed content without blocking it. `allow` accepts it silently and records nothing. Anything else is a startup configuration error; an unset value means `reject`. A registry may narrow or relax its own outcome through `policy_overrides.license_undeclared`, and an unrecognised override is ignored in favour of the global value.

Catalogs that mostly omit license frontmatter are therefore blocked by default, and that is deliberate: relaxing it is a redistribution decision, so it must be made explicitly per instance. Every decision is logged with the resource coordinate, the declared license, whether one was declared at all, and the outcome, so `license_declared=false` separates undeclared resources from disallowed ones in the worker log. A whitespace-only declaration counts as undeclared.

## Storage and operations

Use durable storage for SQLite plus filesystem blobs, PostgreSQL plus S3 blobs, or an equivalent managed service. Configure S3 bucket versioning, encryption, retention, and restricted IAM separately. Monitor `/healthz`, private `/readyz`, worker logs, migration failures, queue age, and blob availability.

Resource ingestion clones configured repositories and therefore requires controlled outbound network access. Limit repository size, file count, file size, artifact size, and operation durations in policy. The service is not a sandbox for arbitrary untrusted network destinations; use egress controls and review registry configuration.

See [security](marketplace-security.md), [backup and restore](marketplace-backup-restore.md), and [reverse proxy](marketplace-reverse-proxy.md).
