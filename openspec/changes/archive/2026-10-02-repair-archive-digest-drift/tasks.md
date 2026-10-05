## 1. Store repair in place

- [x] 1.1 Relax the postgres `putResourceRevision` read-back check: when the
  staged revision matches an existing row's content digest and only
  `archive_digest` differs, update the stored archive digest within the
  publication transaction and log the repair (registry, resource, old and new
  digest); verify with a store test that the publication succeeds and the row
  serves the new digest, annotated
  `// spec: marketplace-publication/drifted-archive-digest-republishes-without-conflict`
- [x] 1.2 Apply the identical relaxation to the sqlite store mirror; verify
  the same store test passes against sqlite
- [x] 1.3 Keep license and description immutable: a staged revision with an
  unchanged content digest but a different license or description still
  returns the publication conflict and leaves the stored row unchanged;
  verify with a store test annotated
  `// spec: marketplace-publication/drifted-license-or-description-still-conflicts`
- [x] 1.4 Audit the refresh path's registry occurrence upsert for its own
  archive-digest enforcement and apply the same relaxation wherever archive
  digests are pinned, so a drifted digest cannot fail an attempt after the
  publication path was fixed; verify with a refresh-path test that a
  previously conflicting revision now records a successful attempt

## 2. Consumer lock-evidence refresh

- [x] 2.1 Replace the resolver's locked-archive-digest mismatch error with
  evidence refresh: for an identical content digest, accept the fetched
  archive digest, refresh the pin's lock evidence, and emit the
  `marketplace.archive-evidence-refreshed` diagnostic; verify with a resolver
  test where the lock pins the old digest and the server returns the new one,
  annotated
  `// spec: marketplace-api/locked-archive-evidence-refreshes-on-identical-content`
- [x] 2.2 Persist the refreshed archive digest through the lock writer on the
  next writable reconciliation; verify with a sync test asserting the lock's
  archive digest advanced while its content digest and byte format stayed
  stable
- [x] 2.3 Refresh archive evidence in `Update`'s unchanged-content skip path
  from the fetched latest revision, without a full re-prepare; verify with a
  pipeline update test that a drifted lock is healed by plain `update`
- [x] 2.4 Keep `sync --locked` strict: on drifted evidence it fails without
  writing, with a message directing the user to plain `sync` or `update`;
  verify with a locked-mode test asserting the actionable error and unchanged
  lock bytes
- [x] 2.5 Cover the production TUI lifecycle (human-confirmed scope): TUI
  sync and update persist refreshed archive evidence through the same
  writable reconciliation, surface `marketplace.archive-evidence-refreshed`
  as a non-fatal warning in the diagnostics overlay, stay quiet on a healthy
  no-op, and fail closed on failed re-acquisition, failed lock publication,
  or a read-only/externally managed lock; verify with the TUI lifecycle
  tests in `cmd/agent-marketplace/tui_lifecycle_test.go`, the warning
  carrier test in `internal/tui/dependency_lifecycle_test.go`, and the
  `marketplace-archive-refresh` tui-qa journey, annotated
  `// spec: marketplace-api/tui-sync-and-update-report-the-refresh-non-fatally`

## 3. Failed attempt cause recording

- [x] 3.1 Extend `FailJob` to persist a stable failure code plus a sanitized
  cause, excluding raw command output and credentials; verify with a store
  test that a failed attempt exposes both fields
- [x] 3.2 Pass the failure cause from the managed worker loop and include it
  in the logged failure line; verify with a worker test asserting the log
  line carries the cause, annotated
  `// spec: marketplace-publication/failed-publication-records-the-conflict-cause`
- [x] 3.3 Expose the failure code and cause on the admin attempts surface and
  add them to the OpenAPI schema (nullable on success); verify with an admin
  API test plus the OpenAPI parity check
- [x] 3.4 Verify a failing acquisition records a sanitized cause without raw
  command output; cover with a refresh-path test annotated
  `// spec: marketplace-publication/acquisition-failure-records-a-sanitized-cause`

## 4. Scenario proof and gates

- [x] 4.1 Add the server-side scenario proof: a publication that stages a
  same-content revision with a repaired archive digest serves the new digest
  while the previously published artifact stays downloadable; annotate
  `// spec: marketplace-api/same-content-revision-carries-a-repaired-archive-digest`
- [x] 4.2 Add an end-to-end regression that reproduces the production state
  (published rows with pre-`support/` archive digests) and proves the next
  publication repairs the digests and publishes previously invisible
  resources; verify it fails before the store change and passes after
- [x] 4.3 Run `make validate` and `make spec-check`; fix findings until both
  pass
- [x] 4.4 Run `openspec archive repair-archive-digest-drift --yes` as the
  final commit of the same pull request
