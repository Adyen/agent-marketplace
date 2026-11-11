## 0. Preconditions

- [ ] 0.1 Confirm against the real Claude Enterprise organization whether an
  administrator can create a second organization-owned `manual` marketplace in
  claude.ai, or whether the library marketplace is the only upload target. Record the
  answer in `design.md` D3. Verify by listing
  `GET /v1/organizations/plugin_marketplaces?owner_type=organization` with a
  `read:plugins` key and comparing against what the claude.ai UI offers
- [ ] 0.2 Confirm the Admin API key, its `read:plugins` and `write:plugins` scopes,
  and the `anthropic-version: 2026-10-21` plus
  `anthropic-beta: ce-plugins-2026-09-01` headers against a live read request.
  Verify `GET /v1/organizations/plugins?limit=1` returns `200`
- [ ] 0.3 Measure the organization's current plugin count and resource count of the
  registries to be mirrored, against the 1,000-plugin marketplace limit (design D2).
  Verify the first full mirror fits, and record the headroom in the change

## 1. Plugins API client (`pkg/marketplace/claudeplugins`)

- [x] 1.1 Add the typed client: base URL, required headers, credential injection,
  context deadlines, and a transport that never logs request bodies or the key.
  Write red tests first for header presence and for redaction of the key in every
  error string. Verify `go test ./pkg/marketplace/claudeplugins -count=1`
- [x] 1.2 Implement reads: list plugins (paginated by opaque cursor until `next_page`
  is null, tolerating short or empty pages), get plugin, list and get versions, list
  marketplaces, get marketplace. Verify a fake server driving a three-page list with
  one empty page yields every row
- [x] 1.3 Implement writes: create plugin, create version (multipart with the archive
  part and `release_notes`), set and remove an installation setting. Verify the
  multipart body carries the archive under the documented field and that
  installation-setting writes are issued one at a time
- [x] 1.4 Implement error typing: status, `error.details.error_code`,
  `details.plugin_id`, `details.plugin_version_id`, `x-should-retry`, and
  `retry-after`. Unknown codes map to their status. Annotate
  `// spec: claude-enterprise-delivery/unrecognized-values-are-opaque`.
  Verify red tests for `plugin_name_taken`, `registration_pending`, `scan_pending`,
  `skill_name_taken`, `429`, and an invented code
- [x] 1.5 Implement rate-limit handling: honour `retry-after`, surface the limit
  headers, and return a typed throttled error that the reconciler can resume from.
  Verify a fake server returning `429` then `200` is handled without a duplicate write

## 2. Configuration (`pkg/marketplace/config`)

- [x] 2.1 Add the `claude_enterprise` block: `enabled`, `api_key` (file or env
  reference reusing the registry credential rules), optional `marketplace_id`,
  `plugin_name_prefix` (default `am`), `default_installation_preference` (default
  `available`), and `interval`. Write red tests first for an inline secret, an unknown
  preference, and an invalid prefix. Annotate
  `// spec: claude-enterprise-delivery/inline-api-key-is-refused-at-validation`.
  Verify `go test ./pkg/marketplace/config -count=1`
- [x] 2.2 Register the key with `internal/redact` and prove it never appears in
  configuration dumps, status output, or error strings. Annotate
  `// spec: claude-enterprise-delivery/status-reports-a-failure-by-code-without-content`
- [x] 2.3 Classify the block in runtime configuration replacement so an enable,
  disable, or credential change takes effect without a restart and an invalid
  replacement keeps the last known-good configuration. Verify the existing
  `marketplace-runtime-configuration` tests still pass and add one for the new block

## 3. Archive assembly and validation (`pkg/marketplace/claudedelivery`)

- [x] 3.1 Implement name derivation from the resource identity: character folding,
  64-character bound, truncation with an identity digest suffix, and the configured
  prefix. Write red tests first for collisions beyond the truncation point. Annotate
  `// spec: claude-enterprise-delivery/a-long-identity-produces-a-bounded-unique-name`
- [x] 3.2 Implement assembly from the stored canonical artifact through
  `artifact.DecodeResource` into the Claude plugin layout for all five kinds, with the
  generated manifest and `release_notes` provenance. Annotate
  `// spec: claude-enterprise-delivery/the-mirrored-resource-keeps-its-own-name`.
  Verify golden archives per kind
- [x] 3.3 Make assembly deterministic: sorted entries, fixed timestamps and
  permissions, fixed compression method. Annotate
  `// spec: claude-enterprise-delivery/repeated-builds-are-byte-identical`.
  Verify two builds in separate processes produce equal digests
- [x] 3.4 Implement local validation of every documented upload requirement, each with
  its own stable code. Write red tests first, one per rule. Annotate
  `// spec: claude-enterprise-delivery/an-invalid-archive-never-reaches-the-api`.
  Verify no upload path is reachable from a failed validation

## 4. Delivery state (`pkg/marketplace/store`)

- [ ] 4.1 Add the `claude_delivery_plugins` schema and migrations for SQLite and
  PostgreSQL, keyed on registry, kind, and resource name, carrying the fields listed
  in `design.md`. Verify the store conformance suite covers both drivers
- [ ] 4.2 Add queries: upsert published, mark retired, mark skipped, mark conflict,
  record failure, list for status, and the desired-set read over current publications
  excluding rejected and revoked revisions. Annotate
  `// spec: claude-enterprise-delivery/a-rejected-resource-is-never-uploaded` and
  `// spec: claude-enterprise-delivery/a-revocation-withdraws-a-mirrored-resource`
- [ ] 4.3 Prove the recorded plugin name is never recomputed for an existing row.
  Annotate
  `// spec: claude-enterprise-delivery/a-renamed-registry-slug-does-not-rename-a-live-plugin`
- [ ] 4.4 Prove sanitized failures: store a stable code and a bounded message, never a
  request body or credential

## 5. Reconciler (`pkg/marketplace/claudedelivery`)

- [ ] 5.1 Implement the preflight: resolve the credential, resolve and verify the
  marketplace, and refuse the pass on a member-owned or synchronized marketplace.
  Annotate
  `// spec: claude-enterprise-delivery/a-synchronized-marketplace-refuses-the-pass`
- [ ] 5.2 Implement inventory read and proof-based adoption. Write red tests first for
  a foreign plugin and for a plugin created by this key. Annotate
  `// spec: claude-enterprise-delivery/a-foreign-plugin-with-the-same-name-is-never-written-to`
  and `// spec: claude-enterprise-delivery/a-plugin-created-by-this-key-is-adopted`
- [ ] 5.3 Implement digest-based change detection and the create or version decision.
  Annotate
  `// spec: claude-enterprise-delivery/a-repeated-pass-over-unchanged-content-writes-nothing`
  and `// spec: claude-enterprise-delivery/a-changed-resource-uploads-exactly-one-version`
- [ ] 5.4 Implement installation-setting reconciliation on create and on withdrawal,
  serialized per plugin, never touching the marketplace default. Annotate
  `// spec: claude-enterprise-delivery/a-newly-created-plugin-becomes-available-to-members`
  and `// spec: claude-enterprise-delivery/a-returning-resource-reuses-its-plugin`
- [ ] 5.5 Implement the retry rules from design D10, each with a red test using a
  fake server. Annotate
  `// spec: claude-enterprise-delivery/a-pending-registration-is-completed-without-a-duplicate-plugin`
  and
  `// spec: claude-enterprise-delivery/a-lost-version-response-does-not-store-a-duplicate-version`
- [ ] 5.6 Record scan status and reach per published version, and never send a
  served-version change. Annotate
  `// spec: claude-enterprise-delivery/a-withheld-version-is-visible-to-the-operator`
- [ ] 5.7 Pace writes under the documented 60-per-minute write limit and resume across
  passes. Annotate
  `// spec: claude-enterprise-delivery/an-interrupted-pass-resumes-rather-than-restarts`

## 6. Worker wiring (`pkg/marketplace/worker`)

- [ ] 6.1 Add the `claude-delivery` job kind with leasing, retry, and single-flight
  per instance, following the managed-job pattern. Annotate
  `// spec: claude-enterprise-delivery/publication-completion-enqueues-delivery`
- [ ] 6.2 Enqueue after publication completion, on revocation change, on the repair
  interval, and on operator demand. Verify no enqueue happens while delivery is
  disabled. Annotate
  `// spec: claude-enterprise-delivery/disabled-delivery-makes-no-outbound-request`
- [ ] 6.3 Add metrics: reconcile outcomes, per-state plugin counts, API requests by
  endpoint and status, throttled requests, and pass duration. Verify they carry no
  resource identity beyond coordinates

## 7. Operator surfaces

- [ ] 7.1 Add `agent-marketplace serve claude-delivery status` and
  `agent-marketplace serve claude-delivery sync --once`, with a behavioral test for
  each public command and flag so `TestPublicCommandPathsHaveBehaviorTests` passes
- [ ] 7.2 Add the read-only admin endpoint listing delivery state, guarded by the
  existing admin authentication and rate limiting
- [ ] 7.3 Make withdrawal reporting state plainly that installed copies and stored
  version history are unaffected. Annotate
  `// spec: claude-enterprise-delivery/status-reports-a-failure-by-code-without-content`

## 8. Documentation

- [ ] 8.1 Document operator setup: Enterprise plan, key creation and rotation, scopes,
  marketplace choice and the recommendation from design D3, and the configuration
  block
- [ ] 8.2 Document the content-scan gap from design D6 and the withdrawal semantics
  from design D8, in the operator documentation and in `status` output

## 9. Gates

- [ ] 9.1 Run `make validate` and `make spec-check`. Verify
  `go run ./tools/specaudit trace` binds every scenario in this change
- [ ] 9.2 Run `go run ./tools/specaudit gate -base origin/main -head HEAD` and confirm
  the spec delta is detected and no TUI journey is required
  (`TUI-QA-Impact: none — no TUI surface changes`)
- [ ] 9.3 Run `make validate-e2e`. Add an E2E scenario driving a fake Plugins API
  through a real `serve` process: first mirror, unchanged pass, changed resource, and
  revocation withdrawal
- [ ] 9.4 Run the pre-push adversarial review over the full diff, covering credential
  handling, adoption proof, retry duplication, and withdrawal semantics
- [ ] 9.5 `openspec archive claude-enterprise-delivery --yes` as the final commit of
  the pull request
