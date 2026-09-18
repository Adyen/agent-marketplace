# Agent readiness quality, governance, and alerting uplift

**Status:** Approved in discussion on 2026-09-18; document review pending.

The readiness assessment identified three independent gaps that can be closed without changing Agent Marketplace product behavior: incomplete quality gates across the Go, Nuxt, and VitePress applications; missing repository governance assets; and missing alert rules and rollback guidance around the hosted Marketplace deployment. This design keeps those concerns as separate implementation slices with one shared verification gate.

## Goals

The change will:

- add an incremental Go complexity policy and a 70% statement-coverage floor over stable library packages;
- add formatter checks to the Nuxt and VitePress applications, plus lint and type checking to VitePress;
- run the new checks in CI and pre-commit without weakening existing tests or dependency security policy;
- add CODEOWNERS, focused pull-request and issue templates, and a four-level priority label set;
- add opt-in Prometheus alerts derived only from the Marketplace API's existing bounded HTTP RED metrics;
- document a Helm rollback procedure that distinguishes Kubernetes manifest rollback from data restoration.

The change will not:

- add Sentry or another vendor error-tracking SDK;
- add automatic error-to-GitHub-issue creation;
- add frontend or VitePress coverage thresholds;
- refactor existing high-complexity product paths solely to make a new linter pass;
- expose `/metrics` through a public listener or ingress;
- add worker alerts before worker metrics exist;
- modify site documentation pages as part of introducing site tooling.

## Quality-gate architecture

### Go lint policy

A root `.golangci.yml` will make the existing implicit golangci-lint behavior explicit. It will retain `errcheck`, `govet`, `ineffassign`, `staticcheck`, and `unused`, then add two bounded checks:

- `gocritic` will enable diagnostic checks rather than broad stylistic rewrites. The existing `exitAfterDefer` finding in the Marketplace container healthcheck will be corrected through a testable return-code seam so deferred cleanup runs before `main` calls `os.Exit`.
- `gocyclo` will inspect non-test functions at a threshold of 75. Test functions are excluded because table-driven contract tests naturally accumulate branches without creating a production maintenance boundary. The current complexity-91 `BuildRegistryRevision` function receives one path-and-message-specific baseline exclusion. The exclusion documents existing debt while ensuring a new function of comparable complexity fails the gate.

The configuration uses golangci-lint v2 syntax and must pass `golangci-lint config verify` under the CI-pinned v2.9.0 binary.

### Go coverage policy

The current stable library scope measures 74.0% statement coverage. A `make coverage` target will:

1. run `CGO_ENABLED=0 go test ./internal/... ./pkg/...` with a temporary cover profile;
2. read the aggregate `total` percentage through `go tool cover -func`;
3. fail when the value is below `COVERAGE_MIN`, which defaults to 70.0;
4. remove the temporary profile on both success and failure.

The coverage target deliberately excludes `./cmd/...`. A repository-wide instrumented `go test ./...` makes the command package's nested process-isolation selection test exceed its intentional 30-second timeout while a lock test is still running. The normal `make test` contract remains unchanged and continues to cover the command packages without instrumentation. Coverage therefore adds a separate regression signal rather than weakening or replacing the existing suite.

The root CI test job will run both `make test` and `make coverage`. Pre-commit will add a distinct coverage hook so a failure is attributable to the coverage policy.

### Nuxt formatting

The web package will add a pinned Prettier dependency and package scripts:

- `format` writes canonical formatting;
- `format:check` checks canonical formatting;
- `check` retains Nuxt preparation, ESLint, Nuxt type checking, OpenAPI drift checking, and Vitest, and adds `format:check`.

A package-local ignore file will exclude dependencies, Nuxt build output, test artifacts, and other generated caches. The initial implementation will format the tracked web source and configuration files once; generated API output remains governed by the existing API drift check.

### VitePress checks

The site package already has a strict TypeScript configuration but does not invoke a type checker. It will add pinned versions compatible with the repository's current Node and Vue stack:

- ESLint 10 with the JavaScript, TypeScript, and Vue flat configurations;
- Prettier 3;
- TypeScript and `vue-tsc` matching the versions already used by the web package.

The package will expose `lint`, `typecheck`, `format`, `format:check`, and an aggregate `check` script. ESLint will cover the VitePress theme, TypeScript data modules, and Vue components. Prettier will cover site source and configuration but exclude `site/docs/**`; documentation prose is outside this slice and is concurrently maintained by other work.

The site workflow will install from the frozen lockfile, run `pnpm check`, then perform the existing VitePress build and link assertions. The build remains an independent gate because successful static generation validates behavior that lint and type checking cannot.

### Local entry points

The Makefile will expose package-level check targets so CI and pre-commit call the same commands. The pre-commit configuration will retain the existing Go test, lint, and build hooks and add Go coverage, web checks, and site checks. Node check targets will install from frozen lockfiles before running package scripts, which makes a clean checkout executable while preserving the configured pnpm release-age policy.

No lockfile-security setting, minimum release age, registry setting, or frozen-lockfile behavior will be relaxed to make these checks pass.

## Repository governance

### Ownership

`.github/CODEOWNERS` will assign the repository-wide default to:

```text
* @Adyen/agent-market-place-collaborators
```

A single owner is preferable to speculative path splits because the repository does not currently expose stable separate maintainer teams for Go, frontend, deployment, or documentation paths.

### Pull-request template

The pull-request template will request only information used during review:

- a concise summary and linked issue;
- user-visible impact;
- verification evidence;
- risk and rollback considerations;
- confirmation that tests, documentation, generated files, and secret handling were considered.

It will not duplicate the repository's coding standards or turn every optional concern into a mandatory claim.

### Issue forms

Two issue forms will cover the repository's common intake paths:

- the bug form captures version, environment, reproduction, expected and actual behavior, and sanitized diagnostics, and applies the existing `bug report` label;
- the feature form captures the user problem, desired outcome, alternatives, and scope, and applies the existing `feature-request` label.

Issue-template configuration will retain blank issues for reports that do not fit either form. Security-report links are omitted because the repository has no published security-reporting destination to reference safely.

### Priority labels

The implementation will create four labels through the GitHub CLI after re-reading the current label set:

- `priority: critical` — immediate action; service, security, or release is blocked;
- `priority: high` — important work to schedule ahead of normal backlog items;
- `priority: medium` — normal planned priority;
- `priority: low` — useful but not time-sensitive.

The labels use distinct red, orange, yellow, and blue colors. Creation is additive. If a same-named label appears concurrently, implementation will preserve it and report the difference instead of overwriting its color or description.

## Alerting architecture

### PrometheusRule boundary

A `PrometheusRule` template will live beside the chart's existing `ServiceMonitor`. It is disabled by default through `prometheusRule.enabled: false`, matching the chart's opt-in monitoring posture. Enabling the rule assumes that Prometheus can already scrape the private operations Service through the ServiceMonitor or an equivalent private configuration.

Every query will select the generated API Service and release namespace. The expressions will use only these existing metric families:

- `up` from the Prometheus scrape target;
- `agent_marketplace_requests_total` for request rate and status;
- `agent_marketplace_request_duration_seconds_bucket` for latency.

The metrics already use bounded `listener`, `method`, `route`, and `code` labels. Alerting will not add raw paths, resource names, registry origins, request headers, or other unbounded dimensions.

### Initial rule set

The initial group contains three alerts:

1. **API target missing** fires at critical severity after five minutes when no successful scrape exists for the release's API Service.
2. **High HTTP 5xx ratio** fires at critical severity when public-listener 5xx responses exceed 5% of public requests for ten minutes. A minimum aggregate request-rate guard prevents isolated low-volume errors from creating a noisy percentage alert.
3. **High p95 latency** fires at warning severity when the public-listener p95 derived from histogram buckets exceeds two seconds for ten minutes. It uses the same minimum request-rate guard.

Durations, thresholds, severity labels, and additional metadata labels will be configurable as typed chart values. The template owns the expressions and does not evaluate arbitrary operator-provided template strings.

Worker failure, queue depth, ingestion failure, and retry-exhaustion alerts remain out of scope because the worker does not yet export stable metric families for those conditions.

### Helm verification

The existing Helm workflow will prove both sides of the feature flag:

- the default render contains no `PrometheusRule`;
- an enabled render contains exactly one rule resource;
- the resource has the expected alert names, namespace and Service selectors, severities, durations, thresholds, and request-rate guards;
- enabling alerts does not change the existing ServiceMonitor's private operations-port selection.

`helm lint --strict`, source-chart rendering, packaged-chart rendering, and existing workload assertions remain intact.

## Rollback model

The Helm README will add an operator procedure covering:

- inspecting revisions with `helm history`;
- reversing to a known revision with `helm rollback --wait --timeout`;
- checking Deployment rollout state, pod health, and private readiness after rollback;
- using `helm upgrade --atomic` for future upgrades where automatic manifest rollback is appropriate.

The guide will state the safety boundary explicitly: Helm restores Kubernetes manifests and the prior pod template, including the chart-managed ConfigMap checksum, but it does not reverse PostgreSQL migrations or restore PostgreSQL and S3 data. Before a potentially incompatible application downgrade, operators must take a data backup and confirm schema compatibility. If data restoration is required, they must use the existing Marketplace backup-and-restore procedure rather than treating `helm rollback` as a database rollback.

## Failure handling

Each slice fails independently and visibly:

- an unreadable or incomplete Go cover profile fails the coverage target rather than reporting zero or passing;
- Node package checks use frozen lockfiles and stop on install, format, lint, typecheck, test, or drift failure;
- Helm alert resources remain absent unless explicitly enabled;
- malformed alert values fail Helm rendering or linting rather than silently producing a partial rule set;
- label creation stops and reports a concurrent same-name label instead of mutating it;
- no error-tracking capability is implied by the existing OpenTelemetry pipeline.

## Alternatives considered

### Gate repository-wide instrumented Go tests

This would make the coverage denominator easy to describe, but it changes timing inside nested command-process tests enough to violate their selection timeout. Raising that timeout would weaken a separate isolation contract. Measuring stable library packages and retaining the normal whole-repository suite preserves both signals.

### Require zero existing complexity findings

Refactoring all current complex paths would mix product-risk changes into a readiness configuration task and trigger unrelated performance obligations. A high but explicit production threshold with one exact baseline exception creates an incremental boundary without disguising the existing outlier.

### Use one root JavaScript toolchain

The web and site applications have separate pnpm versions, lockfiles, build systems, and deployment workflows. A new root workspace would be a structural migration unrelated to readiness. Package-local tools keep each application independently reproducible.

### Add arbitrary Prometheus rules through values

Accepting templated rule bodies would maximize chart flexibility but would move unvalidated PromQL and template execution into values. The chart instead provides a small supported rule set derived from metrics it owns.

### Treat OpenTelemetry as error tracking

The existing OTLP traces provide diagnostic context but do not supply the issue lifecycle, grouping, ownership, or GitHub linking requested by the readiness criterion. Vendor error tracking and automatic issue linking are explicitly deferred rather than claimed indirectly.

## Verification contract

Implementation is acceptable when:

- `golangci-lint config verify` succeeds with v2.9.0;
- `make ci-lint` passes against the implementation base;
- `make test` passes without coverage instrumentation;
- `make coverage` reports at least 70.0% over `./internal/... ./pkg/...`;
- the web frozen install, `pnpm check`, and build pass;
- the site frozen install, `pnpm check`, VitePress build, and existing link assertions pass;
- pre-commit configuration contains attributable Go coverage, web, and site hooks;
- CODEOWNERS and both issue forms name existing GitHub principals and labels;
- all four priority labels exist without overwriting a concurrent definition;
- `helm lint --strict` and the repository's configured and packaged chart render assertions pass for both disabled and enabled PrometheusRule states;
- the Helm guide distinguishes manifest rollback, schema compatibility, and data restoration;
- the complete diff contains no error-tracking SDK, public metrics route, worker alert without metrics, or release-age-policy weakening.
