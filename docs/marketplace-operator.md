# Marketplace operator guide

## Deployment shape

Run the static Go binary as two roles when possible:

- `serve --role api` serves the read-only catalog, artifact, well-known, and public health routes, plus the administration API when `admin.enabled` is true. Browser pages come only from the separate Nuxt frontend, so `/r/**` returns 404 from this role.
- `serve --role worker` acquires, analyzes, and indexes repositories. Only the worker image (the `worker` target of `Dockerfile.marketplace`) contains Git and its CA bundle, because acquisition runs there. It also contains `ssh` when the build image provides it.
- `serve --role all`, the default when `--role` is omitted, runs both in one process. It suits a single-process or development deployment and is not the preferred production topology. Build it from the `worker` target so that Git is available.

The API and worker can start at the same time. The SQLite and PostgreSQL migration runners serialize schema changes and record the versions they apply. PostgreSQL uses a transaction-scoped advisory lock; SQLite uses an immediate transaction, WAL, a busy timeout, and retries. A failed migration rolls back its transaction and blocks startup, so the service never runs on a partial schema.

## Network boundary

Public catalog APIs have no built-in user authentication or authorization. Put them behind a TLS-terminating, authenticated network boundary, or keep them on a private network, and restrict the proxy port with firewall or security-group policy. The optional administration API authorizes identities through exactly one configured mode: a signed session cookie issued by native OIDC Authorization Code + PKCE sign-in (`admin.authentication.mode: oidc`), or identity asserted by an explicitly trusted immediate proxy (`trusted_proxy`, which is inferred when the complete set of proxy fields is present). [Enable and use the admin panel](../site/docs/how-to/operator/enable-and-use-the-admin-panel.md) describes the operator contract.

`operations_listen` serves `/readyz` and `/metrics`. It defaults to loopback. Do not publish it through a container port or reverse proxy; the public listener returns 404 for both paths. Collect metrics from a private sidecar or network path with its own access controls. `/metrics` exposes `agent_marketplace_requests_total` (labels `listener`, `method`, `route`, `code`), `agent_marketplace_request_duration_seconds`, and `agent_marketplace_active_requests`, plus the standard Go and process collectors. It has no queue or job metrics.

`/healthz` is a liveness endpoint and does not prove storage readiness. `/readyz` returns 200 only when configuration authority is available, live revocations are reconciled, the instance record exists, and a blob-store probe succeeds. The container healthcheck, `/app/marketplace-healthcheck`, calls `http://127.0.0.1:9090/readyz`, so keep `operations_listen` on port 9090 and reachable on loopback inside the container when you use it.

## Catalog projection consumers

The catalog projection endpoint is for internal consumers such as Backstage. The application does not authenticate it.

The projection contains only resources from registries that this hosted Marketplace instance has approved and indexed. It never includes local registries, personal directories, direct Git sources, or ad-hoc CLI `--from` registries, and consumers must not treat CLI-discovered or locally configured registries as projection inputs.

Each current resource anchor has one of six kinds: `skill`, `command`, `subagent`, `hook`, `mcpserver`, or `collection`. Blocked and revoked anchors stay in the projection with their availability state, so a consumer can tell them apart from a resource that has disappeared. Consumers must keep and reconcile those anchors. Neither an anchor's absence nor its state is an instruction to discard its identity without trace.

Rows may include the sanitized source repository URL, the repository-relative source path, and the exact indexed commit. These fields can reveal repository structure, so confirm that the gateway policy allows that disclosure before exposing them to a wider audience.

### Scan and compatibility contract

Consumers read the projection as a stateless, bounded keyset scan. Start without a cursor, then pass the opaque cursor from each response until the final page. Marketplace keeps no cursors, snapshots, marks, sweep state, or any other per-consumer synchronization state.

The scan moves with the data instead of reading a consistent snapshot. Record the global ETag on the first and final pages. If they differ, schedule a catch-up scan, because additions behind the cursor, updates, or removals may otherwise only converge on the next cycle. Send `If-None-Match` only on the first page of a new scan, using the ETag from the last completed ingestion. Continuation requests carry only their cursor and no conditional validators.

Before enabling a consumer, verify that it supports the endpoint's advertised API version and the `catalog-projection-v1alpha1` capability. The endpoint is disabled by default. Enable it with:

```yaml
catalog_projection:
  enabled: true
```

While it is disabled, `/api/v1/catalog` is not routed and the catalog-projection feature is absent from both the capabilities response and the well-known document. Keep the consumer disabled until it passes its version and capability check and this setting is on.

## Configuration and secrets

When browser pages are deployed, set `instance.browser_base` to the public Nuxt base, including its path prefix (for example, `https://marketplace.example/marketplace`). Omit it for an API-only deployment. With it set, the well-known document includes separate latest and exact browser route templates.

Use `storage.metadata.dsn_file` in production, preferably through a Compose or platform secret integration. Configure exactly one DSN source, and make sure the selected value is not empty. A literal `dsn` and the environment-based `dsn_env` Compose fixture are for local development only. DSN values never appear in configuration errors or application startup logs.

Protect DSN files with file permissions and a secret manager. For S3, prefer workload identity or IAM roles through the AWS SDK's default credential chain. If roles are unavailable, use an SDK-supported credentials file from the platform secret manager instead of static environment credentials. Do not put passwords in committed YAML, Compose files, image layers, command arguments, or issue output.

A registry `origin` must be an HTTP(S) URL with a host. It must not contain URL userinfo, a fragment, or password or token query parameters. Put registry authentication in `credentials` instead, which accepts only file or environment references and rejects inline secret values.

Production SQLite configuration must name a writable database file. The `:memory:` DSN and the `mode=memory`, `mode=ro`, and `immutable=1` URI options are rejected. (Tests can still use the SQLite package's direct in-memory constructor.)

A registry that uses a private certificate authority can set `ca_file` to the absolute path of a readable PEM bundle that is a regular file, not a symlink, and contains at least one certificate. For each acquisition, the worker copies the validated bundle to a private file and passes that copy as `GIT_SSL_CAINFO` only to the Git subprocesses for that registry. It does not change global Git configuration or apply the bundle to other registries. Every role validates the operator configuration at startup, so mount the file read-only at the same path in every API and worker container:

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

When different registry paths publish the same kind and registry-local name, ingestion keeps the first occurrence in configured root order and logs `marketplace.ingest.duplicate-resource` with both paths and a remediation: give the resources distinct registry-local names. Listing the same path twice is an operator configuration error.

The API reads revocation declarations when it starts. After changing `revocations`, restart every API process with the new configuration; a reindex does not apply the change to a running API.

## Exercise database-managed administration in an isolated deployment

The managed backend can run after explicit adoption, but the admin product as a whole is experimental. Full resource diagnostics and impact analysis, and complete browser acceptance testing, are not finished. Try this procedure on a disposable copy of metadata and blobs first. Completing it does not approve a production rollout.

### Prepare the deployment and identity boundary

1. Run the same managed-capable release for every API, worker, and maintenance process. Do not mix old workers with an adopted database.
2. Keep storage, listener addresses, instance identity, global limits other than license policy, and deployment revocations in operator YAML. Add `management.mode: database` and `management.allowed_origins`, which lists the exact allowed protocol, host, and port authorities without repository paths. Database mode refuses to start before adoption.
3. If a registry uses credentials or a private CA, add an entry in `management.profiles` with its `allowed_origins`, `credentials`, and optional `ca_file`. Adoption requires exactly one profile that matches the registry's existing credential and trust references. Mount those references on every worker. Do not send secret values or filesystem paths to the browser.
4. Configure `admin.enabled`, an HTTPS `admin.origin` without a path, and `admin.admin_groups` or `admin.admin_users`, then select exactly one authentication mode.
   - For `admin.authentication.mode: trusted_proxy` (inferred when the complete set of proxy fields is present and no mode is set), also configure `admin.trusted_proxies` with the immediate proxy CIDRs, `admin.issuer`, `admin.subject_header`, and `admin.groups_header`. The proxy must strip identity headers sent by the client and set them itself after authentication. The subject header carries one stable subject. The groups header carries a JSON string array such as `["marketplace-admins"]`, not a comma-separated list. Never trust a forwarded client IP as the immediate peer.
   - For `admin.authentication.mode: oidc`, configure `admin.authentication.oidc` instead: `issuer`, `client_id`, exactly one of `session_secret_env` or `session_secret_file`, and optionally `groups_claim` and `session_ttl`. OIDC mode rejects the trusted-proxy identity fields. [Enable and use the admin panel](../site/docs/how-to/operator/enable-and-use-the-admin-panel.md) describes the native sign-in flow and the Okta client contract.
5. In trusted-proxy mode, protect both `/api/v1/admin/**` and the browser's `/marketplace/admin/**` routes at the gateway, and block any path around the gateway. In OIDC mode, the gateway only routes admin traffic to the application and must not inject identity headers. Keep admin requests same-origin. Public server-side rendering must not create or forward an admin identity. Admin reads and writes are private and must not be stored in shared caches.

### Adopt during a maintenance window

1. Stop all API and worker processes. Take coordinated metadata and blob backups as described in the [backup guide](marketplace-backup-restore.md).
2. Invalidate the shared caches you control that may hold previously public immutable artifacts. Confirm that the proxy revalidates managed content through the API, including HEAD and conditional requests, and that direct object-store URLs cannot bypass availability. Content that was already downloaded, and browser caches, cannot be recalled.
3. Keep every stored registry identity in the adoption YAML. Run the maintenance command with an operator identity, a reason, and a stable retry key:

   ```sh
   agent-marketplace serve adopt --config marketplace.yaml \
     --actor operator-id --reason 'Adopt catalog management' \
     --idempotency-key maintenance-cutover-1 --confirm-maintenance
   ```

   Expected output begins `Adopted marketplace configuration at revision 1.` Ownership backfill runs in bounded batches before the authority transaction. The `--confirm-maintenance` flag is your acknowledgement; the command does not verify shutdown, backups, or cache invalidation. If any step fails, keep the processes stopped and fix the failure. Then retry with the same inputs and key, because adoption may have committed.

4. Start the API and worker roles with `management.mode: database`. Confirm that the private `/readyz` succeeds, that the authenticated configuration read returns the adopted revision, and that an untrusted direct admin request is denied. Missing or corrupt authority state, or missing runtime store capabilities, block startup. File mode cannot reopen an adopted database.
5. Verify an indexing attempt and a public resource read. In an isolated acceptance fixture, pause refresh and confirm that existing content still serves, withdraw it and confirm that downloads are denied, then restore availability and confirm that the registry stays paused. A successful configuration request does not mean a successful publication.

### Operate after adoption

Database settings own registry configuration, lifecycle, and license policy, and restarting with stale registry YAML does not overwrite them. Save the adoption input separately, then remove obsolete registry entries from the runtime YAML; configuration loading still validates any entries left in that file. Keep the deployment-owned profile definitions and the limits other than license policy current.

`serve reindex --config marketplace.yaml` selects active managed registries, not the YAML registry list. A named registry is matched against its managed ID or current slug and is still subject to worker lifecycle checks. Disabled or withdrawn registries are not resumed implicitly. Scheduled and manual work reads live settings and rechecks publication authority in a transaction. In file mode, the same command reindexes every configured registry, or only the one named by slug or ID. In both modes, the command has a one-hour timeout and ends with an integrity check of metadata against blobs.

Use configuration activity and `/api/v1/admin/attempts` separately. An accepted change can have a failed or superseded attempt, and a failed attempt does not erase the previous successful publication. Attempt history accepts `registry_id`, `job_id`, a `before` cursor, and a `limit` from 1 to 100 (default 50). It is not a worker heartbeat.

To turn off the administration interface, set `admin.enabled: false` and keep database mode. Disabling the interface does not give authority back to the YAML file; returning to file authority needs a separately reviewed export and cutover. Do not delete the adoption marker, and do not restore metadata without its associated blobs.

## License policy

`policy.license_allowlist` lists the licenses this instance is willing to index and mirror. It defaults to `[MIT, Apache-2.0, BSD-2-Clause, BSD-3-Clause]`. An empty list turns license enforcement off: nothing is rejected or reported on license grounds, whatever a resource declares.

While an allowlist is configured, ingestion treats two conditions differently:

- A disallowed resource declares a license that the allowlist does not contain. The operator has seen that license and excluded it, so the resource is rejected, its intrinsic artifact is blocked from serving and installation, and it is reported as `marketplace.policy.license-not-allowed` with the declared license.
- An undeclared resource declares no license at all. Nothing is known about its terms, which differs from knowing they are unacceptable, so the operator chooses the outcome. It is reported as `marketplace.policy.license-undeclared`, never as `license-not-allowed`.

`policy.license_undeclared` selects that outcome and accepts `reject`, `warn`, or `allow`:

```yaml
policy:
  license_allowlist: [MIT, Apache-2.0]
  # reject (default) | warn | allow
  license_undeclared: reject
```

The default is `reject`: an undeclared license under a configured allowlist blocks the resource. `warn` keeps the resource installable and records a warning-severity finding, so operators can inventory unlicensed content without blocking it. `allow` accepts it without recording anything. Any other value is a startup configuration error, and an unset value means `reject`. A registry can tighten or relax its own outcome through `policy_overrides.license_undeclared`. An unrecognized override value is ignored with a logged warning, and the global value applies.

As a result, catalogs that mostly omit license frontmatter are blocked by default. Relaxing that is a redistribution decision, so each instance must make it explicitly. Each rejected or warned decision is logged with the resource coordinate, the declared license, whether a license was declared at all, and the outcome, so `license_declared=false` separates undeclared resources from disallowed ones in the worker log. Allowed decisions are logged in the same shape, but only at debug level. A declaration that contains only whitespace counts as undeclared.

## Storage and operations

Use durable storage: SQLite with filesystem blobs, PostgreSQL with S3 blobs, or an equivalent managed service. Configure S3 bucket versioning, encryption, retention, and restricted IAM separately. Monitor `/healthz`, the private `/readyz`, the request metrics, and the worker logs for migration failures, indexing progress, and blob availability. In database mode, `/api/v1/admin/attempts` also shows indexing attempt history.

Resource ingestion clones the configured repositories, so it needs controlled outbound network access. Set limits on repository size, file count, file size, artifact size, and operation durations in `policy.limits`. The service is not a sandbox for arbitrary untrusted network destinations, so apply egress controls and review the registry configuration.

See [security](marketplace-security.md), [backup and restore](marketplace-backup-restore.md), and [reverse proxy](marketplace-reverse-proxy.md).
