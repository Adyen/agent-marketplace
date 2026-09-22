# Operator-exposed rejected Marketplace resources

Date: 2026-09-22
Status: approved

## Problem

A Marketplace resource assessment is immutable evidence about one exact resource revision. Today, an assessment decision of `reject` makes that revision `policy-blocked`: the public API lists the resource for transparency but refuses its metadata, inspection projection, manifests, content, and artifact. The CLI cannot install it, and `--accept-risk` cannot bypass the rejection.

This fail-closed default is appropriate for a public Marketplace, but it leaves no operator-controlled distribution decision for an internal policy domain. An operator who has independently reviewed a rejected resource must either change the analyzer or ingestion policy and reindex, modify the source, or bypass the Marketplace by installing directly from Git. Those alternatives conflate different decisions:

- the assessment records what analyzers and ingestion policy concluded;
- distribution policy records whether the Marketplace operator chooses to expose that evidence-bearing revision.

The system needs a typed, auditable, fail-closed distribution override that preserves the rejection instead of rewriting it as a warning or an allow decision.

## Goals

- Let an operator expose rejected resources globally or for one registry.
- Support every Marketplace resource kind, including recursive collections.
- Make operator-exposed resources normally installable without an additional user consent prompt.
- Preserve the immutable reject assessment and findings.
- Make the override visible in search results, resource details, and administration.
- Support both file-managed and database-managed Marketplace authority.
- Apply policy changes immediately without reindexing.
- Keep revocation, withdrawal, integrity verification, and malformed-artifact failures non-bypassable.

## Non-goals

- Changing analyzer verdicts, finding severity, or assessment identity.
- Allowing consumers to override Marketplace policy with `--accept-risk`.
- Recalling or deleting bytes that consumers installed before policy tightened.
- Exposing resources that failed before a publishable resource revision and artifact existed.
- Replacing the analyzer plugin and audit architecture proposed by pull request #237.
- Adding per-finding-code exposure policy in the first version.

## Core model

### Assessment and distribution remain separate

A resource assessment remains the authority for analyzer and ingestion-policy evidence. An assessment with decision `reject` stays rejected permanently. The new policy affects only effective distribution availability.

The effective states are:

| State | Installable | Meaning |
|---|:---:|---|
| `available` | yes | The assessment and current operator policy permit distribution. |
| `operator-exposed` | yes | The assessment rejects the revision, but the effective Marketplace distribution policy exposes it. |
| `policy-blocked` | no | Current policy refuses distribution. |
| `no-artifact` | no | A mirrored resource has no artifact to serve. |
| `revoked` | no | An effective operator revocation or registry lifecycle state blocks distribution. |
| `blocked` | no | A compatibility or aggregate state cannot express a narrower reason. |

`operator-exposed` is deliberately distinct from `available`. Clients can enable installation while displaying an **Operator-exposed rejection** badge. Resource details continue to show the assessment decision and findings.

### Policy syntax

File-managed Marketplace configuration gains a fail-closed enum:

```yaml
policy:
  rejected_resources: block
```

Accepted values are:

- `block`: current behavior and the default when omitted;
- `expose`: make rejected, otherwise valid resources `operator-exposed`.

A registry may override the global value:

```yaml
registries:
  - id: harness
    policy_overrides:
      rejected_resources: expose
```

A missing registry override inherits the global value. Unknown global values fail configuration validation. Unknown or malformed typed admin values fail mutation validation. Existing managed snapshots that omit the field resolve to `block`.

Resolution lives behind one policy interface used by list projection, coordinate-scoped resource routes, digest ownership checks, and clients. Callers do not reinterpret findings.

## Configuration authority

### File-managed mode

`config.Policy` owns the global `rejected_resources` value. Registry `policy_overrides` accepts the same key. Validation and effective resolution use one implementation so ingestion, API serving, and documentation cannot drift.

Changing file-managed policy requires the normal deployment configuration reload or process rollout. It does not require reindexing because rejected mirrored artifacts and inspection blobs are already retained.

### Database-managed mode

The managed snapshot gains typed distribution policy rather than hiding the setting in `NonLicenseOverrides` or attaching it to `LicensePolicy`.

The managed model contains:

- a global rejected-resource mode;
- an optional rejected-resource override on each registry;
- the existing policy revision used for availability cache invalidation and audit history.

Version-1 snapshots remain readable. An omitted global field resolves to `block`, and an omitted registry field means inheritance. Adoption copies file-managed values into typed managed state.

Global and registry changes use the existing optimistic-concurrency, reason, actor, request-ID, and idempotency contracts. A distribution-only change increments the managed policy/configuration revision but does not enqueue reindexing work. The API evaluates it from current managed state on every availability-sensitive request.

## Administration interface

### Global policy

The **Policies** page gains a **Rejected resources** field:

- **Block publication**;
- **Expose as operator-approved**.

Selecting global exposure requires an explicit acknowledgment that rejected resources from every registry may become downloadable and installable. The existing Review step displays the before/after value and requires an audit reason.

### Registry override

The registry Configuration page gains **Rejected resource availability**:

- **Inherit global policy**;
- **Block**;
- **Expose as operator-approved**.

The field is typed and editable for existing and new registries. It is not rendered inside the preserved read-only `non_license_overrides` object. Registry tables and details identify when an explicit override differs from inherited policy.

Applying the `harness` override makes its qualifying rejected resources effective immediately. The UI does not claim that a reindex completed or that the assessment changed.

### Public presentation

Search cards and resource details display **Operator-exposed rejection** for `operator-exposed`. Install controls remain enabled. The assessment and findings remain visible on detail pages. The badge must not use language such as “safe,” “approved assessment,” or “passed.”

## Availability resolution

Effective availability follows this precedence:

1. malformed or unavailable managed state fails closed;
2. effective revision, coordinate, or registry revocation returns `revoked`;
3. withdrawn or retired managed registries return `revoked`;
4. current license rejection returns `policy-blocked` unless rejected-resource exposure applies;
5. immutable assessment rejection returns `policy-blocked` or `operator-exposed` according to effective exposure policy;
6. a required mirrored artifact that is absent returns `no-artifact`;
7. otherwise the revision is `available`.

The exposure policy is broad by design. It applies to published reject assessments from analyzers, license policy, hidden-content policy, executable assets, and artifact-size policy. Administration must describe this breadth. It does not override structural parsing failures, acquisition failures, missing blobs, digest mismatches, or schema validation.

Tightening from `expose` to `block` immediately denies new metadata and blob reads. Relaxing from `block` to `expose` immediately permits them. Neither transition mutates assessments or requires a registry refresh.

## Route authorization

An `operator-exposed` coordinate may read the same public surfaces as an available coordinate:

- exact and latest resource metadata;
- revision history;
- assessments and findings;
- inspection manifests and bounded inspection content;
- registry and collection manifests needed for acquisition;
- mirrored artifacts and content-addressed blobs.

Revoked, missing, malformed, or integrity-invalid data remains unavailable.

Digest-addressed routes have no coordinate in their URL, so authorization uses all current owners:

1. any effective revocation denies the digest;
2. otherwise, at least one `available` or `operator-exposed` owner authorizes serving the identical bytes;
3. if every owner is blocked, access is denied;
4. an unowned digest never causes a blob-store read.

This rule allows a per-registry override to work when identical bytes also occur in a blocked registry. The exposed owner is explicit operator authority for those bytes. Revocation remains conservative because it represents a recall decision rather than an assessment opinion.

Denied requests preserve the existing no-blob-read invariant. Allowed requests retain range, conditional request, cache-control, content-type, and digest verification behavior.

## Client behavior

### Protocol compatibility

The public availability enum adds `operator-exposed`. OpenAPI and generated clients describe it as installable with a retained reject assessment.

Older list clients already discard unknown string availability values instead of interpreting them as available. Older installers may continue to fail closed when they see the reject assessment. New clients recognize the server-side distribution authorization.

### CLI and TUI

Marketplace preparation carries effective availability alongside the immutable revision. The resolver permits a reject assessment only when the response proves effective availability is `operator-exposed`. It continues to reject a bare reject assessment, an unknown availability value, or inconsistent server responses.

An operator-exposed install:

- does not require `--accept-risk` or interactive consent solely because of reject findings;
- still verifies Marketplace identity, coordinate, revision digest, archive digest, schema, provenance, and materialization safety;
- renders the operator-exposed status and findings in verbose preview/detail output;
- records the exact Marketplace pin and assessment provenance as before.

`--accept-risk` retains its existing meaning for warning findings and active capabilities. It does not become a general policy bypass.

Updates may advance to another `operator-exposed` revision normally. A transition to `policy-blocked`, `revoked`, malformed, or unverifiable remains an error. Existing installed bytes are not recalled or automatically removed.

### Collections

A collection install evaluates the root revision and every recursively expanded member under each owning registry’s effective policy. The collection is installable only when every node is `available` or `operator-exposed`.

A blocked or revoked member fails the plan before its bytes are fetched or materialized. Errors identify the member coordinate and availability reason. Collection manifests remain subject to digest and graph validation.

## Relationship to analyzer plugins

Pull request #237 changes how analyzer audits are configured, executed, stored, and displayed. It intentionally makes analyzer enablement instance-global, preserves assessment decisions as install authority, and keeps reject findings non-consentable. It does not provide per-registry distribution overrides or cover license and artifact policy rejection.

The two designs are complementary:

- analyzer plugins determine and explain assessments;
- rejected-resource exposure determines effective distribution after assessment.

If the analyzer plugin work lands later, its audit badges and detail pages remain visible for operator-exposed resources. The exposure policy must not rewrite audit verdicts or `consentable` markers.

## Failure modes

- Invalid authored enum values reject configuration or admin mutation; they never enable exposure.
- Missing fields in legacy managed snapshots resolve to `block`.
- Corrupt managed snapshots make availability unavailable rather than exposed.
- A registry override references only its immutable registry ID, so slug changes do not move policy.
- A failed admin mutation does not increment revision or change availability.
- A race between policy change and a request resolves against one loaded managed snapshot; existing request-level availability context provides that consistency.
- Shared digests remain denied when any owner is effectively revoked.
- Unsupported clients fail closed rather than silently installing an exposed rejection.

## Observability and audit

Managed changes retain actor, issuer, reason, request identity, before/after snapshot, and policy revision through existing audit history. Structured logs record accepted global and registry exposure changes without logging finding payloads or resource content.

Search and catalog continue to expose low-cardinality availability. Existing request metrics can distinguish status codes; adding a new unbounded resource label is out of scope. A bounded `operator-exposed` availability value may be included wherever availability is already recorded.

## Testing strategy

Implementation follows test-first development.

### Configuration and administration

- omitted, `block`, `expose`, and invalid global values;
- registry inheritance and both explicit overrides;
- adoption into managed state;
- legacy snapshot defaulting;
- global and registry admin validation, review, apply, history, and restore;
- optimistic concurrency and idempotency;
- UI global acknowledgment and per-registry edit/review/apply flows.

### API and storage authorization

- list, search, and catalog projection of `operator-exposed`;
- exact/latest metadata, revision lists, assessment, inspection, manifest, content, and artifact routes;
- `GET`, `HEAD`, conditional, and range requests;
- denied requests perform zero blob reads;
- missing and unowned blobs remain unavailable;
- shared digest matrices for available, exposed, blocked, and revoked owners;
- withdrawal and revocation precedence;
- immediate block/expose transitions without publication changes.

### Clients

- web parsing, badges, detail findings, and enabled install controls;
- CLI and TUI installation without consent for proved `operator-exposed` revisions;
- continued refusal for ordinary `policy-blocked`, revoked, unknown, malformed, and unverifiable revisions;
- warning/capability consent remains unchanged;
- update and status transitions;
- recursive collections with mixed registry policies.

### Broader verification

- focused Go package tests for config, admin, worker, API, Marketplace resolver, pipeline, and TUI;
- OpenAPI generation and contract tests;
- web unit/component tests and focused Playwright coverage;
- database adapter tests for managed snapshot persistence;
- Marketplace lifecycle E2E coverage;
- repository lint, build, and applicable compatibility gates.

The availability path is user-visible and runs on every Marketplace request. Existing availability benchmarks must be extended, or a focused benchmark added, before implementation changes are evaluated. Baseline and post-change latency and allocation results belong in the implementation handoff.

## Alternatives rejected

### Rewrite rejected assessments as warnings

Changing ingestion output would erase the distinction between assessment evidence and distribution authority, change assessment identity, require reindexing, and make later policy tightening unable to recover the original judgment.

### Require consumer risk acceptance

A query parameter or `--accept-risk` path would make the consumer the policy authority, complicate cache identity, and contradict the selected centrally managed trust model. Warning consent remains separate.

### Make analyzer outcomes configurable instead

Analyzer configuration cannot cover license, artifact-size, and future non-analyzer policy findings. Instance-global analyzer changes also cannot express a registry-specific exception. Analyzer flexibility is useful independently but does not replace distribution policy.

### Present exposed resources as ordinary `available`

Collapsing the state would hide the operator override from search and detail views. A distinct installable state preserves transparency without adding user consent.

## Documentation impact

Implementation updates the operator configuration reference, admin-panel how-to, ingestion-policy guide, Marketplace installation reference, API/OpenAPI description, and security explanation. Procedure belongs in how-to documentation; enum syntax and defaults belong in reference documentation; the assessment-versus-distribution rationale belongs in explanation documentation.
