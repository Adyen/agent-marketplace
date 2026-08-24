# Transparent Telemetry

## Status

Approved scope for epic `agent-marketplace-d5u.1.10`.

This plan covers CLI telemetry, its configuration and transparency surfaces, the
hosted community receiver, and acceptance work. Implementation is split into
child beads so the public telemetry contract lands before instrumentation.

## Goals

- Measure Agent Marketplace adoption and command health without collecting
  command arguments or resource identifiers at the basic level.
- Give opted-in users and managed organizations lifecycle traces that help guide
  product development.
- Let a user capture one problem with a reviewable diagnostic payload and a
  trace reference that support can look up.
- Export through vendor-neutral OTLP/HTTP using official OpenTelemetry Go SDKs.
- Make collection authority, destination, payload categories, retention policy,
  installation identity, and delivery state inspectable.
- Keep telemetry failure from changing normal command output or exit status.

## Non-goals

- Harness runtime analytics.
- A generic secret-detection or log-scrubbing library.
- Raw prompts, resource contents, terminal contents, subprocess output, or
  arbitrary `slog` export during normal collection.
- Repository-controlled telemetry policy.
- Vendor-specific telemetry SDKs.

## Domain model

### Levels

The persistent level ordering is:

1. `off`
2. `basic`
3. `detailed`

`diagnostic` is a per-invocation overlay selected with `--diagnostic`. It is not
a persistent level and cannot be enforced by organization policy.

Signal mapping:

| Level | OpenTelemetry signals |
|---|---|
| Basic | Metrics |
| Detailed | Basic metrics plus traces |
| Diagnostic | Basic metrics, detailed traces, and diagnostic logs/events |

All users may select basic or detailed telemetry. An organization may enforce a
minimum of basic or detailed.

### Routes

The **primary telemetry route** receives the effective level:

- On an unmanaged installation, it uses the user endpoint when configured,
  otherwise the published project endpoint.
- On a managed installation, an explicitly configured organization endpoint
  overrides and locks the user endpoint.

The **community report** contains only the basic schema and goes to the
published project endpoint:

- On an unmanaged installation, it requires the user's explicit choice.
- On a managed installation, organization policy enables or disables it; the
  user cannot override that decision.
- When the primary route already uses the project endpoint, the CLI exports
  once instead of duplicating the basic data.
- Detailed and diagnostic data are never copied into the community report.

Diagnostic telemetry always uses the primary route.

## Consent and enforcement

Interactive `init` explains the basic categories, project destination, and
retention policy, then asks once whether to enable basic telemetry. The default
answer is Yes. The choice is written only after an affirmative interaction.
Non-interactive `init` does not prompt and leaves telemetry unset, which behaves
as off.

Organization configuration must activate telemetry explicitly. Merely declaring
an endpoint does not activate collection. An omitted minimum level is off and
an omitted `community_reporting` value is false.

The effective primary level is the higher of the user-selected level and the
organization minimum. Organization endpoint, minimum, and community-report
fields cannot be overridden by user config or environment variables. Disabling
telemetry changes only user-controlled routes; status must still report routes
that remain active because of organization policy.

## Configuration contract

Telemetry configuration is machine-scoped:

- Organization and User Personal layers may contain `telemetry`.
- Repository Committed and Repository Personal layers must reject it.
- Organization configuration supplies locked policy.
- User configuration supplies preferences where organization policy is silent.
- Documented `AGENT_MARKETPLACE_TELEMETRY_*` environment variables may override
  user preferences only on unmanaged policy fields.
- Standard `OTEL_*` variables must not silently bypass Agent Marketplace policy.

Illustrative shape:

```yaml
telemetry:
  level: detailed
  endpoint: https://otel.example.com
  community_reporting: true
  retention_policy: https://example.com/telemetry-retention
  ca_file: /etc/example/otel-ca.pem
  allow_insecure: false
  headers:
    Authorization:
      env: EXAMPLE_OTEL_AUTHORIZATION
    X-Tenant:
      file: /etc/example/otel-tenant
```

The final schema may refine field names, but must preserve these rules:

- Inline secret header values are forbidden.
- Header values may come from environment variables or permission-restricted
  files.
- Status and preview show header names and source types, never values.
- A retention-policy URL is required whenever an organization enables its
  endpoint.
- The project endpoint and retention-policy URL are release configuration, not
  organization defaults hidden in the binary.

Transport requires HTTPS for non-loopback endpoints. Plain HTTP is accepted for
loopback. Organization configuration may explicitly allow a private insecure
endpoint. System trust is used by default, a CA file may extend trust, and
certificate verification can never be disabled.

## Installation identity

The installation ID is a random, visible, resettable identifier generated lazily
when telemetry first becomes active. It lives under the user's durable state
root, not config or cache, and is not a user identity or device fingerprint.

`telemetry reset-id` purges pending records carrying the old ID before creating
a new one. IDs do not rotate automatically.

The installation ID is the one intentional high-cardinality resource attribute
on basic metrics. It must not also be attached as an instrument-level metric
label. Exact resource IDs, URLs, paths, and trace IDs never appear as metric
dimensions.

## Schema contract

Agent Marketplace telemetry has a schema version independent of CLI, config API,
OTLP, and OpenTelemetry SDK versions. Every record includes
`telemetry.schema.version`. Additive changes retain the major version; removals
or semantic changes require a new major version and a backend compatibility
period.

### Basic metrics

Basic collection is a stable, low-cardinality invocation schema:

- CLI version
- OS and architecture
- command name, never arguments
- configured harness types
- managed or unmanaged status
- controlled outcome code
- coarse duration
- installation ID as the intentional resource-identity exception

It excludes resource and registry names, URLs, paths, Git data, usernames,
hostname, raw errors, prompts, subprocess data, and content.

The initial instruments should be a command invocation counter and duration
histogram under an `agent_marketplace` namespace. Instrument attributes must be
bounded enums. The operations backend must explicitly budget and validate the
installation-ID resource cardinality.

### Detailed traces

Detailed mode adds:

- exact command and boolean/enum flag usage, never argument values
- root command or TUI-session traces
- lifecycle spans for config load, registry cache, catalog ingest, selection,
  materialization, backup and ownership, update, and delivery
- exact resource kind/name and registry alias on spans
- item counts, warnings, controlled error codes, and precise timing
- exact filesystem paths
- URL scheme, hostname, and path

URL userinfo, query, and fragment are structurally omitted. Credentials remain
excluded from normal detailed collection.

The TUI records semantic actions such as browse, filter, install, remove, sync,
modal outcome, and controlled errors. It never records keypresses, typed search
text, terminal contents, or selection names at basic level.

### Diagnostic capture

`agent-marketplace --diagnostic <command>` buffers metrics, traces, and useful
diagnostic config/error/log evidence for that invocation. It performs only
narrow best-effort cleanup, such as removing credentials from recognized URLs;
it does not promise comprehensive secret detection.

Before export, the CLI:

1. Writes the exact proposed values to a user-only local file.
2. Shows the file path, destination hostname, retention-policy link,
   installation ID, and trace ID.
3. Warns that the payload may contain sensitive information.
4. Requires confirmation.

Non-interactive use requires a dedicated acknowledgement flag in addition to
`--diagnostic`. Organization policy cannot provide this acknowledgement.

After confirmation, diagnostic export may wait up to five seconds. It prints
success or failure plus the trace ID as the diagnostic reference and the
installation ID. Export failure never replaces the underlying command result.
The local payload is deleted after successful export and retained with its path
reported after failure. Diagnostic payloads never enter the normal queue.

## Delivery and durable state

Official OpenTelemetry Go SDKs and OTLP/HTTP exporters sit behind a small
internal telemetry interface. Command and pipeline packages must depend on that
interface rather than SDK types.

Normal basic and detailed delivery is asynchronous while a command runs and
receives a bounded 250–500 ms exit flush. Failure:

- never changes normal output or exit status
- is available in debug logging and `telemetry status`
- writes the record to a bounded user-only queue

The queue lives under the durable state root and is capped at 5 MiB and seven
days, evicting oldest first. Every enabled invocation attempts a short
oldest-first drain before current export. Records retain their original
timestamp, schema version, route, and installation ID. Endpoint changes do not
reroute old records; they expire naturally or are purged.

`telemetry purge` removes pending records and retained diagnostic payloads
without changing configuration.

## CLI transparency

Add:

- `telemetry enable --level basic|detailed [--endpoint URL]`
- `telemetry disable`
- `telemetry status`
- `telemetry preview --level basic|detailed|diagnostic`
- `telemetry reset-id`
- `telemetry purge`
- global `--diagnostic`
- `--diagnostic-accept-sensitive-data` for non-interactive acknowledgement

`telemetry status` shows:

- effective primary level and its authority/source
- primary destination hostname
- community-report status and authority
- installation ID
- retention-policy link
- pending record count and oldest age
- retained diagnostic payloads
- last delivery failure

`telemetry preview` renders a representative local payload and never sends it.
Telemetry management commands do not report themselves. `init` may report only
after the user's choice has been committed.

## Instrumentation seams

- Initialize and close telemetry beside root logging in Cobra
  `PersistentPreRunE`/`PersistentPostRunE`.
- Preserve the underlying command error until telemetry finish has recorded the
  controlled outcome.
- Instrument pipeline result boundaries rather than exporting arbitrary slog
  records.
- Use one root trace for a command and one session trace for the TUI.
- Keep logging and telemetry independent; telemetry outages must not alter log
  availability.

## Performance, reliability, and tests

Each implementation child owns unit tests and the benchmarks required by the
repository performance policy.

Minimum acceptance coverage:

- config layer validation, precedence, environment restrictions, and managed
  locks
- init consent in interactive and non-interactive modes
- route matrix, including endpoint deduplication and community basic-only export
- no telemetry before consent on unmanaged installations
- organization-enforced minimum and community policy
- basic attribute allowlist and high-cardinality guard
- detailed span inventory and TUI semantic-action exclusions
- diagnostic review, confirmation, non-interactive acknowledgement, trace ID,
  failure retention, and underlying exit-code preservation
- bounded flush, queue cap/age/eviction, route preservation, reset, and purge
- TLS, CA, insecure-private exception, and auth-source handling
- OTLP receiver contract tests with local HTTP servers
- command-path and TUI instrumentation benchmarks
- security review of config, queue permissions, diagnostic disclosure, endpoint
  routing, and dependency additions

## Epic decomposition

1. **Telemetry contract and ADR**: levels, routing, precedence, config schema,
   signal/schema contract, and security boundaries.
2. **Telemetry runtime core**: official SDK abstraction, installation ID,
   exporters, route handling, bounded queue, and retries.
3. **Telemetry controls and consent**: init prompt, enable/disable/status/preview,
   reset/purge, and managed-policy explanations.
4. **Basic instrumentation**: command lifecycle metrics and bounded attributes.
5. **Detailed instrumentation**: CLI/pipeline traces, lifecycle spans, and
   semantic TUI actions.
6. **Diagnostic capture**: `--diagnostic`, buffered evidence, exact-payload
   review, confirmation, and diagnostic reference.
7. **Community telemetry operations**: receiver/backend, dashboards, retention
   policy, release endpoint, and cardinality budget.
8. **Epic acceptance**: E2E route/consent/offline tests, benchmarks,
   security/privacy review, and user/operator documentation.

Dependencies:

- Children 2, 3, and 7 depend on child 1.
- Child 4 depends on children 2 and 3.
- Child 5 depends on child 4.
- Child 6 depends on children 2 and 5.
- Child 8 depends on children 1–7.
