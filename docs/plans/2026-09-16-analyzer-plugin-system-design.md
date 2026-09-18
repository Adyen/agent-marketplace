# Analyzer plugin system design

Date: 2026-09-16
Status: implemented

## Problem

The Marketplace security analyzer is one fixed suite behind six entry points.

- `analyze.NewEngine()` hardcodes six per-kind analyzers (`engine.go`). `Register` is
  exported but only the constructor calls it, and no configuration path can add, enable, or
  disable anything.
- All six call `baseAnalyze`, which runs the same nine lexical scanners (`analyzer.go`,
  `scanners.go`) and then adds one or two kind-specific capability findings. There is
  effectively one analyzer.
- Provenance is collapsed. `analyzerName` prefers `opts.Analyzer`, ingest sets that to
  `"marketplace.analyze"` for every resource, and `toStoreFindings` does not persist an analyzer
  name at all (`ingest.go`). Every finding on the instance is attributed to one
  anonymous analyzer at one global `AnalyzerVersionV1 = "1.2.0"`.
- Policy is a flat `code -> reject|warn|allow` map with defaults in `defaultOutcomes`
  (`policy.go`). Operators can configure `hidden_content`, a license allowlist, and limits;
  `Registry.PolicyOverrides map[string]any` is an untyped escape hatch that ingest interprets for
  three specific keys.
- Because there is one version for the whole suite, changing any single detection invalidates
  every assessment on the instance.

The result is low value and no flexibility: the findings are lexical signals
(`html-comment`, `remote-reference`, protocol-relative `//`) that fire on ordinary documentation
and land in a warn bucket nobody acts on, plus capability disclosures
(`mcp-capability`, `hook-capability`) reported as warnings even though they describe what the
resource legitimately is. There is no named check a user can trust or an operator can turn off.

## Goal

Make analyzers a plugin system: many named, independently versioned analyzers, each individually
enabled, configured, and assigned a gating or advisory role by the operator, each producing its own
verdict, with per-analyzer results stored and displayed. Operators must be able to add an analyzer
without building the binary.

The user-facing model is skills.sh's "Security Audits" row: several named auditors, each with a
verdict badge and its own detail page, rather than one flat findings list.

## Non-goals

- Third parties submitting audit records into the instance through an authenticated API. The
  out-of-process runner covers the same need with the marketplace remaining the scheduler, and an
  inbound provider API is a separate trust, identity, and staleness problem.
- Per-registry or per-kind analyzer selection. Enablement is instance-global in this design.
- Replacing the revision-level assessment. It remains the authority for install gating.
- WASM or dynamically loaded Go plugins.

## Vocabulary

An **analyzer** is the plugin: an identity, a version, a role, and configuration. An **audit** is
one analyzer's verdict about one resource revision. Operator configuration says `analyzers:`; the
web UI says "Security audits".

## Design

### 1. The analyzer contract

`pkg/marketplace/analyze` gains one contract that both runtimes implement.

```go
// Analyzer is one independently identified, versioned, and configured security
// opinion about a resource revision.
type Analyzer interface {
	ID() string                  // "marketplace.hidden-content", "acme.socket"
	Version() string             // bumped independently of every other analyzer
	Kinds() []ir.ResourceKind    // applicability; empty means every kind
	Analyze(ctx context.Context, s Subject, cfg AnalyzerConfig) (Audit, error)
}

// Subject is the immutable content one analyzer inspects.
type Subject struct {
	Resource ir.Resource
	Files    FileSet
	Registry Registry
	Limits   Limits
}

// Audit is one analyzer's verdict.
type Audit struct {
	AnalyzerID      string
	AnalyzerVersion string
	Role            string // gating | advisory
	Verdict         string // pass | warn | fail | error | skipped
	Summary         string // one human sentence rendered on the badge
	Findings        []Finding
	FindingCount    int
	DurationNS      int64
	Error           string // redacted; set only when Verdict is error
	ResultDigest    string // sha256:<64 lowercase hex>
}
```

`Finding` gains `AnalyzerID` so attribution survives into the store, which it does not today.

`Engine` replaces `map[ir.ResourceKind][]Analyzer` with one ordered registry filtered by
`Kinds()`. This also removes the current `no analyzer registered for kind %q` failure mode
(`engine.go`): a kind with no applicable analyzer yields zero audits, not an error.

`Engine.Analyze` returns `[]Audit` plus the derived rollup, so the caller never has to re-derive a
decision.

### 2. Splitting the existing suite into named analyzers

Five built-in analyzers. Every existing `marketplace.find.*` code string is kept verbatim, so the
store rows, `codes.md` parity test (`codes_test.go`), the openspec code requirement, and every
web and CLI assertion continue to hold. Only the owning analyzer changes.

| Analyzer ID | Codes | Default role |
|---|---|---|
| `marketplace.hidden-content` | `html-comment`, `zero-width`, `unsafe-bidi`, `terminal-control` | gating |
| `marketplace.obfuscation` | `obfuscated-instruction`, `dynamic-execution` | gating |
| `marketplace.assets` | `executable-asset`, `executable-script`, `asset-path-escape` | gating |
| `marketplace.references` | `dangerous-url`, `remote-reference`, `suspicious-path` | advisory |
| `marketplace.capabilities` | `tool-grant`, `hook-capability`, `mcp-capability` | advisory |

The per-code outcomes in `defaultOutcomes` are preserved, so the union of findings and the derived
decision for a resource are unchanged by the split alone.

`marketplace.capabilities` becoming advisory is the substantive behavior change and the reason the
current output has little value: "resource declares MCP server capability" is disclosure, not a
detection, and today it makes every MCP server and every hook a warning. As an advisory audit it
renders as a capability statement instead of a warning that trains users to pass `--accept-risk`.

License and artifact-size policy stay in ingest (`ingest.go`) and are not analyzers. They
are decisions about configuration, not about content, and they already have their own
`marketplace.policy.*` vocabulary.

Third-party analyzers namespace their codes as `<analyzer-id>.<code>`. The openspec requirement
that every code carry the `marketplace.find.` prefix narrows to built-in analyzers.

### 3. Two runtimes

**In-process.** The five built-ins, plus a rulepack-driven built-in
(`marketplace.rulepack`) that loads operator-authored pattern files, giving pattern-only authoring
without a Go build.

**Out-of-process.** A runner modeled on the existing hardened subprocess pattern in
`acquire.runGitInput` (`acquire/acquire.go`): `exec.CommandContext` with the per-analyzer
timeout, an empty environment except explicitly declared passthrough names, an isolated temp
working directory, no inherited credentials, and errors redacted before they reach a stored audit.

Protocol: one JSON request on stdin, one JSON audit on stdout.

- Request carries the resource IR, the primary content, and support files inline, each bounded by
  `policy.limits` (`file_bytes`, `file_count`, `repository_bytes`), plus the analyzer's opaque
  `config` block and the request schema version.
- Response is bounded in size. A response that is malformed, oversized, or reports a version other
  than the configured one produces an `error` audit rather than being trusted.
- stderr is captured, truncated, and redacted into the audit's `Error` field.

The runner echoes its configured version in the response; this validates the response against the
configured protocol identity, but it does not establish executable identity. Every out-of-process
analyzer declares a canonical sha256 `implementation_digest` in `run.implementation_digest`; this
digest participates in the audit `config_digest`. Before invocation the marketplace hashes the
executable at `run.command` and refuses to launch it when the actual bytes do not match the
declared `implementation_digest`.

### 4. Operator configuration

A typed `analyzers` field on `config.Policy`, validated in `Policy.Validate`. Unknown IDs are a
configuration error, consistent with the existing `decoder.KnownFields(true)` strictness
(`config.go`).

```yaml
policy:
  analyzers:
    - id: marketplace.capabilities
      role: advisory                # gating | advisory
    - id: marketplace.references
      enabled: false
    - id: acme.socket
      role: advisory
      version: "2.1.0"              # runner must echo this back
      run:
        command: /usr/local/libexec/am-audit-socket
        args: ["--json"]
        timeout: 30s
        env_passthrough: [SOCKET_TOKEN_FILE]
        implementation_digest: sha256:<64 lowercase hex>
      config:                       # opaque to the marketplace; hashed into config_digest
        tier: enterprise
```

Omitting `policy.analyzers` yields the five built-ins at their default roles, so upgrading an
existing deployment does not change its behavior. Validation covers: known or `run`-backed IDs,
unique IDs, valid roles, positive timeouts, absolute command paths, non-empty declared versions for
out-of-process analyzers, and no secret literals in `config` or `env_passthrough` values.

### 5. Identity, storage, and result reuse

New table `resource_audits`, added as `migration1` appended to both migration slices
(`store/sqlite/migrate.go`, `store/postgres/migrate.go`), keyed by:

```
(marketplace, registry, kind, name, subject_digest, analyzer_id, analyzer_version, config_digest)
```

carrying role, verdict, summary, findings JSON, duration, error, and `assessed_at`.

**`subject_digest` is a new digest, not `content_digest`.** `BuildResourceArtifact` computes
`ContentDigest` from `hccartifact.FromIR(r)` alone (`artifact/builder.go`); the support
`FileSet` is passed to the analyzer but never enters that digest. Keying the cache on
`content_digest` would therefore reuse a stale audit after a support file changed. `subject_digest`
is a canonical sha256 over the artifact content digest plus the sorted support-file path and digest
pairs that `supportFileDigests` already computes (`ingest.go`).

`config_digest` is a canonical sha256 over the resolved analyzer entry (role, version, opaque
config, and the effective `analyze.Policy` outcomes that analyzer can emit).

Result reuse: before running an analyzer, ingest looks up
`(subject_digest, analyzer_id, analyzer_version, config_digest)`; a hit is reused verbatim.
Enabling one new analyzer therefore runs exactly that analyzer across the registry instead of
re-running everything, and a policy edit reruns only the analyzers whose `config_digest` changed.
Error audits are never cached.

`ResourceAssessment` keeps its current shape and role as the rollup:

- `findings` remains the union of all audit findings, so the web findings list, CLI preview, and
  `e2e/marketplace_lifecycle_test.go` keep seeing what they see today.
- `analyzer_version` becomes a deterministic audit-set identity, `audits:sha256:<hex>` over the
  sorted `id@version+config_digest` triples. The field stays a non-empty string, which is all the
  store (`assessment.go`) and OpenAPI (`analyzer_version: {minLength: 1}`) require.
- `policy_digest` extends to include the resolved analyzer set, so `resourcePolicyDigest`
  (`ingest.go`) covers enablement and role changes.

Because assessment identity already binds `analyzer_version` and `policy_digest`
(`store/assessment.go`), changing the analyzer set produces a new assessment digest and a new
current assessment, which is exactly the existing reindex behavior asserted by
`e2e/marketplace_lifecycle_test.go`.

### 6. Decision fold and failure semantics

The revision decision is derived from the audits:

1. any gating audit with verdict `fail` -> `reject`
2. otherwise any gating audit with verdict `warn` or `error`, or any advisory audit with `fail` or
   `warn` -> `warn`
3. otherwise -> `allow`

An advisory analyzer can never reject. That is the whole point of the role.

A gating analyzer that times out, crashes, or returns an unusable response yields an `error` audit
and downgrades the revision to `warn` rather than rejecting it, so one misconfigured binary or one
expired third-party token cannot silently reject an entire registry.

Fail-soft alone would open a bypass: a resource crafted to reliably crash the analyzer that would
have caught it would be downgraded from `reject` to `warn` and become installable with
`--accept-risk`. So an `error` audit on a **gating** analyzer emits a **non-consentable** finding,
`marketplace.policy.audit-unavailable`. The resource stays visible and inspectable, its metadata
and inspection preview render normally, and installation refuses until the analyzer succeeds.
`internal/pipeline/install_marketplace.go` already distinguishes reject (never consentable,
`:641-650`) from warn (consentable, `:652-655`); this adds a third case: warn-severity findings
carrying a non-consentable marker are treated like reject for consent purposes while leaving the
assessment decision at `warn`.

### 7. API

Additive only.

- `ResourceRevision` gains `audits[]`, each entry carrying `analyzer_id`, `analyzer_version`, `role`,
  `verdict`, `summary`, `finding_count`, `duration_ns`, and `result_digest`. Findings themselves stay in
  `assessment.findings` and carry `analyzer_id`; a `Finding` may optionally carry `consentable: false` to
  mark non-consentable findings that `--accept-risk` cannot bypass.
- New route `GET /api/v1/resources/{registry}/{kind}/{name}/revisions/{digest}/audits/{analyzer_id}`
  returns one audit with its findings and is immutable for the same `result_digest`.
- `AssessmentFinding` gains optional `analyzer_id` and optional `consentable`.
- `api/openapi.yaml` declares `Audit` and `AuditSummary`.

### 8. Web

- New badge row on `ResourceDetailView.vue`, above the existing findings section: one badge per
  audit with verdict color, analyzer name, and summary, linking to the per-audit page.
- New page under the existing `/r/{registry}/{kind}/{name}/revisions/{digest}/` tree rendering one
  audit: verdict, analyzer identity and version, role, summary, its findings with the existing
  source-range highlighting, and its error text when it failed.
- The existing flat findings list stays and is grouped by analyzer, so
  `resource-detail-view.test.ts` assertions on the `Security findings` heading, `Severity:`,
  `Policy:`, code, message, and range text keep passing.
- Gating authority is unchanged: `presentation.ts:209-219` continues to treat the assessment
  decision as authoritative and finding severities as presentation data. Audit verdicts are
  presentation data too; the decision is the fold computed server-side.
- `validation.ts` validates `audits[]` shape, bounds its length, and keeps rejecting legacy
  top-level `analyzer_version`/`findings`.

### 9. CLI and TUI

- The install preview groups findings by analyzer and names the analyzer that refused, using the
  existing preview builder (`install_marketplace.go`).
- Non-consentable audit-unavailable findings print an explicit line stating that `--accept-risk`
  cannot clear them.
- The TUI inherits all of this through the rendered consent preview string
  (`internal/tui/discovery.go`), so no TUI model change is required.

## Data flow

```
worker.Refresh
  -> acquire (unchanged)
  -> ingest.BuildRegistryRevision
       resolve analyzer set from config -> []resolvedAnalyzer (id, version, role, config_digest)
       per resource:
         subject_digest = H(content_digest + sorted support digests)
         per analyzer:
           cache hit  -> reuse stored audit
           cache miss -> in-process call, or hardened subprocess
         fold audits -> decision, union findings
         apply license/artifact policy (unchanged)
         write assessment (rollup) + resource_audits rows
  -> store.PublishRegistryRevision (unchanged)
```

## Testing

- `pkg/marketplace/analyze`: per-analyzer unit tests replacing the current suite-wide tests;
  registry filtering by kind; the decision fold across every role and verdict combination;
  `codes.md` parity extended to record each code's owning analyzer.
- Runner tests: successful audit, timeout, non-zero exit, oversized response, malformed JSON,
  version mismatch, and a runner attempting to read an environment variable that was not declared.
- `pkg/marketplace/ingest`: cache hit and miss keyed on each of subject digest, analyzer version,
  and config digest; the support-file-changed case that motivated `subject_digest`; error audits not
  cached; policy digest changes when the analyzer set changes.
- `pkg/marketplace/store`: migration applies on both drivers; audit rows are immutable; assessment
  identity still changes when the audit-set identity changes.
- `pkg/marketplace/api`: the new audit route, immutable caching, and 404 for an unknown analyzer ID;
  `openapi_test.go` schema presence.
- `web`: component test for the badge row and the per-audit page; validation tests for `audits[]`
  bounds; `mock-marketplace.mjs` extended with two audits so the browser E2E covers a pass and a
  fail badge.
- `e2e`: a `TestCase` in `e2e/table_test.go` covering an operator disabling a gating analyzer and a
  previously rejected resource becoming installable, plus an out-of-process analyzer configured to
  fail and the resulting non-consentable install refusal.
- Benchmarks, per repository policy: `BenchmarkEngineAnalyze` over the built-in set and
  `BenchmarkAuditCacheLookup` on the ingest path, with before and after numbers recorded in the
  implementation commit. Analysis runs for every resource of every registry refresh, so it is a hot
  path by the policy's definition.
- Logging, per repository policy: `slog` at analyzer start and end with ID, version, verdict, and
  duration; cache hit and miss at debug; runner failures at warn with redacted output.

## Documentation and spec updates

- `pkg/marketplace/analyze/codes.md`: add the owning analyzer to each code row and document the new
  `marketplace.policy.audit-unavailable` code.
- `openspec/specs/marketplace-protocols/spec.md`: narrow the `marketplace.find.` prefix requirement
  to built-in analyzers, and add requirements for audit identity, the gating and advisory roles, and
  the derived decision fold.
- `docs/marketplace-operator.md`: the `policy.analyzers` reference and how to add an out-of-process
  analyzer.
- `docs/site/how-to/install-from-a-marketplace.md` and `docs/site/reference/cli.md`: per-analyzer
  preview output and the non-consentable audit-unavailable case.
- `AGENTS.md`: replace the implicit single-analyzer description with the plugin model.

## Security considerations

- An out-of-process analyzer is operator-supplied code running on the marketplace host. The design
  does not sandbox it beyond process isolation, an empty environment, a timeout, and a temp cwd;
  configuring one is a deliberate operator trust decision, and the documentation must say so
  plainly.
- Analyzer requests carry resource content only. Registry credentials, the metadata DSN, and the
  marketplace environment are never passed; `env_passthrough` names are explicit and validated.
- Audit summaries, findings, and error text are persisted in public revision metadata. The existing
  rule that MCP credential fields never reach finding messages (`mcpserver.go`) applies to
  audits as well, and runner stderr is truncated and redacted before storage.
- Findings remain bounded by `MaxFindings`; `audits[]` is bounded by the configured analyzer count,
  and per-audit findings are bounded before the rollup union is truncated.

## Risks

- **Fail-soft plus non-consentable is a new consent state.** Three consumers read consent semantics
  (web, CLI, TUI). If the non-consentable marker is not honored in all of them, the bypass this
  design closes reopens. The e2e case for it is mandatory, not optional.
- **Cache correctness depends on `subject_digest` being complete.** If a future change feeds an
  analyzer any input not covered by the digest, stale audits will be served. The digest builder and
  the analyzer `Subject` must be constructed from the same value.
- **Splitting the suite changes finding attribution in stored rows.** Existing assessments keep
  `marketplace.analyze` as their analyzer version identity; only new assessments carry the audit-set
  identity. Both forms must render.
