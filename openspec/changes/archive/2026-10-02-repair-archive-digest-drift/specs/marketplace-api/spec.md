## MODIFIED Requirements

### Requirement: Canonical artifact format

A resource artifact SHALL be a deterministic `tar.gz` archive under artifact
schema v1 containing:

- `manifest.json` with schema, kind, name, revision, digests, provenance, and
  optional capabilities/findings
- `resource/<kind>.json` canonical payload
- optional `assets/<relative-path>` files
- optional `licenses/<relative-path>` files
- optional `support/<url-escaped-relative-path>` files carrying the declared
  support bytes and executable modes of a mirrored skill; declared paths may
  be flat or nested and are url-escaped into member names

`support/` members remain within artifact schema v1 but change the archive
digest. The archive digest is encoding evidence, not revision identity: a
revision whose content digest is unchanged MAY carry a repaired archive
digest produced by a newer artifact encoding, and the Marketplace repairs
such a revision in place while retaining the previously published artifact
(see the `marketplace-publication` capability). Clients predating `support/`
members fail canonical digest validation on such archives rather than
silently installing them. A client whose locked Marketplace record pins an
archive digest that the current revision metadata for the same content digest
no longer carries MUST refresh the lock's archive evidence and re-acquire
the resource artifact, reporting the refresh, rather than failing closed; a
last-good installation is preserved when re-acquisition fails.

#### Scenario: Canonical artifact round-trip

- **WHEN** a resource artifact is built from any materialized resource kind
  (`skill`, `command`, `subagent`, `hook`, or `mcpserver`)
- **THEN** encoding MUST produce a deterministic `tar.gz` archive and an
  archive digest
- **AND** decoding MUST restore the manifest, payload, assets, licenses, and
  support files
- **AND** content-digest, archive-digest, and archive validation MUST pass for
  the decoded artifact

#### Scenario: Payload contains resource name

- **WHEN** a resource artifact is built from a resource
- **THEN** the canonical `resource/<kind>.json` payload MUST contain the
  resource name

#### Scenario: Security rejection during decode

- **WHEN** a `tar.gz` containing absolute paths, `..` path components,
  symlinks, hard links, or device/special files is decoded
- **THEN** decoding MUST fail before the unsafe entry is accepted

#### Scenario: Oversized resource rejection

- **WHEN** archive validation runs with a payload or asset exceeding the
  configured archive limits
- **THEN** it MUST fail with an error identifying the oversized resource

#### Scenario: Archive digest verification catches archive tampering

- **WHEN** archive digest verification runs on a resource artifact whose
  manifest `archive_digest` does not match its canonical archive
- **THEN** it MUST fail with an archive digest mismatch
- **AND** when raw archive bytes that differ from the canonical archive are
  supplied, it MUST fail even if the manifest digest is correct
- **AND** an untampered encoded artifact MUST verify

#### Scenario: Same-content revision carries a repaired archive digest

- **WHEN** a published resource revision's artifact encoding changes so that
  the same content digest yields a new archive digest
- **THEN** the revision MUST remain identified by its content digest
- **AND** the Marketplace MUST serve the repaired archive digest as the
  revision's archive digest
- **AND** the previously published artifact MUST remain content-addressed and
  downloadable

#### Scenario: Locked archive evidence refreshes on identical content

- **WHEN** a locked Marketplace record pins an archive digest and the fetched
  revision metadata for the identical content digest carries a different
  archive digest
- **THEN** the client MUST refresh the lock's archive evidence to the fetched
  archive digest and re-acquire the resource artifact
- **AND** the client MUST report the refresh rather than fail resolution
- **AND** the lock's content digest for the resource MUST remain unchanged

#### Scenario: TUI sync and update report the refresh non-fatally

- **WHEN** the interactive TUI runs a sync or update and a locked
  Marketplace record pins an archive digest that the fetched revision
  metadata for the identical content digest no longer carries
- **THEN** the TUI MUST persist the refreshed archive evidence to the
  writable lock and report the refresh as a non-fatal
  `marketplace.archive-evidence-refreshed` warning visible in the
  diagnostics view
- **AND** a healthy sync or update without drift MUST complete without a
  refresh warning or lock write
- **AND** a failed re-acquisition or a lock managed by a read-only scope
  MUST fail closed, preserving the last-good installation and the
  unchanged lock
