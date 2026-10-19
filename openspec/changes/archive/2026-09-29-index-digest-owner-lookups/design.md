# Design: index-digest-owner-lookups

## Context

Digest-owner resolution asks the metadata store: "which resource revisions in
this Marketplace carry content digest X?" Both store backends (PostgreSQL and
SQLite) answer it from `resource_revisions` and, where assessments are
resolved alongside, `current_resource_assessments`. Neither table's primary
key leads with `content_digest`, so the lookup previously degraded with total
retained history rather than with the number of matching rows.

## Decisions

- **D1: Specify the index contract, not the migration.** Governance forbids
  specifying migration history, so the spec states the durable, observable
  contract, that non-unique (Marketplace, content digest) indexes exist on
  the relevant relations in each backend, proven by asserting the index
  definitions after opening a migrated store. Index names are asserted only
  as test evidence, not as normative spec content. Table and column names
  are observable schema state (a downstream consumer reads them through
  `sqlite_master` or `pg_indexes`), so naming them in scenarios specifies
  observable state, not migration history. The spec makes no query-plan or
  latency claim; the bounded-lookup benefit stays in the proposal as
  motivation because no annotated test proves it.
- **D2: Non-unique, composite ordering.** Many resource revisions share no
  digest and a digest may map to several revisions, so the index is
  non-unique and leads with the Marketplace column to match how every digest
  query is scoped.
- **D3: Keep the pre-existing SQLite revision index.** SQLite
  `resource_revisions` already carried a digest index before this change;
  the spec requires exactly one such index to remain, making accidental
  removal or duplication detectable.
- **D4: No latency claim.** The retained PostgreSQL benchmark measures the
  lookup but, at 8 samples per side with p=0.083, does not establish a
  statistically significant improvement. The spec therefore claims index
  presence only; it asserts no production latency reduction.
