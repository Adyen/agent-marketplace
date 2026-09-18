# Marketplace operator guide

## Deployment shape

Run the static Go binary as two roles when possible:

- `serve --role api` serves read-only catalog, artifact, well-known, and public health routes. Browser pages are served only by the separate Nuxt frontend; `/r/**` returns 404 from this API-only role.
- `serve --role worker` performs repository acquisition, analysis, and indexing. The worker image includes `git`, CA certificates, and `/bin/sh` because acquisition runs there.
- `serve --role all` is suitable for a single-process development deployment, not the preferred production topology.

The API and worker can start concurrently. SQLite and PostgreSQL migration runners serialize schema changes and record applied versions. PostgreSQL uses a transaction-scoped advisory lock; SQLite uses an immediate transaction, WAL, busy timeout, and retry handling. A failed migration rolls back its transaction and should block startup rather than run with a partial schema.

## Network boundary

The application has no built-in user authentication or authorization. Treat every public API and browser route as unauthenticated. Put it behind a TLS-terminating, authenticated network boundary, or keep it on a private network. Restrict the proxy port with firewall/security-group policy.

`operations_listen` serves `/readyz` and `/metrics`. It defaults to loopback and must not be published through a container port or reverse proxy; the public listener returns 404 for both paths. If operators need metrics, collect them from a private sidecar/network path with its own access controls. `/healthz` is a liveness endpoint and does not prove storage readiness. Container API healthchecks use the private `/readyz` listener.

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

## Analyzer policy

`policy.analyzers` configures the security-analyzer plugin set. Omitting the field keeps the five built-in analyzers at their default roles. An explicit empty list disables analysis entirely.

| Analyzer ID | Default version | Default role | Built-in codes |
|---|---|---|---|
| `marketplace.hidden-content` | `1.2.0` | gating | `marketplace.find.html-comment`, `marketplace.find.zero-width`, `marketplace.find.unsafe-bidi`, `marketplace.find.terminal-control` |
| `marketplace.obfuscation` | `1.2.0` | gating | `marketplace.find.obfuscated-instruction`, `marketplace.find.dynamic-execution` |
| `marketplace.assets` | `1.2.0` | gating | `marketplace.find.executable-asset`, `marketplace.find.executable-script`, `marketplace.find.asset-path-escape` |
| `marketplace.references` | `1.2.0` | advisory | `marketplace.find.dangerous-url`, `marketplace.find.remote-reference`, `marketplace.find.suspicious-path` |
| `marketplace.capabilities` | `1.2.0` | advisory | `marketplace.find.tool-grant`, `marketplace.find.hook-capability`, `marketplace.find.mcp-capability` |
| `marketplace.rulepack` | `1.0.0` | gating | operator-defined |

Each entry selects an analyzer by `id`, optionally overrides `role` (`gating` or `advisory`), sets `enabled: false` to disable it, and, for out-of-process analyzers, supplies a `run` block.

### Example

```yaml
policy:
  analyzers:
    - id: marketplace.capabilities
      role: advisory
    - id: marketplace.references
      enabled: false
    - id: custom.sast
      enabled: true
      role: gating
      version: "2.1.0"
      run:
        command: /usr/local/libexec/custom-sast
        args: ["--json", "--config-file", "/etc/custom/sast.json"]
        timeout: 30s
        env_passthrough: [SAST_TOKEN_FILE]
        implementation_digest: sha256:<64 lowercase hex>
      config:
        threshold: high
    - id: marketplace.rulepack
      rulepack:
        path: /etc/agent-marketplace/rulepack.yaml
```

Do not place literal secrets in `config` or `args`; secret-bearing keys must use a single `file:` or `env:` reference.

### Validation and behavior

- IDs must be known built-ins, `marketplace.rulepack`, or have a `run` block.
- Out-of-process analyzers must declare `role` and `version`; the runner must echo the version back.
- `run.command` must be an absolute path and the file at that path must match `implementation_digest`.
- `run.timeout` must be positive.
- `run.env_passthrough` is an ordered allowlist of environment variable names passed to the runner.
- `run.args` are passed in order; they may not contain inline secret literals or a secret option followed by a literal value.
- `config` is opaque to the marketplace and participates in the analyzer's cache key.
- Built-in versions are fixed. An explicit `version` equal to the built-in is accepted; any other value is rejected. `version` is not configurable for `marketplace.rulepack`.
- A gating analyzer that cannot complete produces a non-consentable `marketplace.policy.audit-unavailable` finding. The resource remains visible and auditable, but it cannot be installed until the analyzer succeeds.

### Caching and retry

Each audit is keyed on the `subject_digest` (content plus support files) and a `config_digest` that covers the resolved analyzer entry, its opaque `config`, and the effective policy outcomes. Changing any of those values, or an out-of-process runner timeout, invalidates the cache for that analyzer only. Error audits are not cached; the next refresh re-runs the analyzer and the API `GET .../audits/{analyzer_id}` returns the new result. Reindexing the identical config and content reuses cached audits byte-for-byte.

### Audit API and UI

The public API and web detail page list one badge per audit with its verdict, role, analyzer identity and version, summary, `finding_count`, `duration_ns`, and a deterministic `result_digest`. The per-analyzer detail route is `GET /api/v1/resources/{registry}/{kind}/{name}/revisions/{digest}/audits/{analyzer_id}` and is immutable for the same `result_digest`.

### Rollout and troubleshooting

Start with the default set to preserve existing behavior. Introduce a new out-of-process analyzer in `advisory` role first, then switch it to `gating` after the runner is verified. If a gating analyzer fails, the install preview and the TUI show the non-consentable `marketplace.policy.audit-unavailable` finding and refuse `--accept-risk` or `--yes` until the analyzer succeeds. Worker logs include the analyzer ID, version, verdict, and duration; runner stderr is redacted before storage.

## Storage and operations

Use durable storage for SQLite plus filesystem blobs, PostgreSQL plus S3 blobs, or an equivalent managed service. Configure S3 bucket versioning, encryption, retention, and restricted IAM separately. Monitor `/healthz`, private `/readyz`, worker logs, migration failures, queue age, and blob availability.

Resource ingestion clones configured repositories and therefore requires controlled outbound network access. Limit repository size, file count, file size, artifact size, and operation durations in policy. The service is not a sandbox for arbitrary untrusted network destinations; use egress controls and review registry configuration.

See [security](marketplace-security.md), [backup and restore](marketplace-backup-restore.md), and [reverse proxy](marketplace-reverse-proxy.md).
