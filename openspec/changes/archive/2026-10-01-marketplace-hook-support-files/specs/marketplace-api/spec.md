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
  support bytes and executable modes of a mirrored skill or hook; declared paths
  may be flat or nested and are url-escaped into member names

`support/` members remain within artifact schema v1 but change the archive
digest; because `archive_digest` is immutable per content digest, they can
only ship under a new content revision. Clients predating `support/` members
fail canonical digest validation on such archives rather than silently
installing them. An already-published artifact that declares support files but
does not carry `support/` members cannot be repaired in place; consumers MUST
fail closed, preserving any last-good installation, until a new content
revision or an index-only source supplies the bytes.

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

## ADDED Requirements

### Requirement: Mirrored hooks carry integrity-bound support files

Mirror acquisition MUST package the adjacent support files selected by the
source-layout hook materialization policy, excluding native aggregate
definitions. Their relative paths, bytes, and executable modes MUST be bound
into the canonical hook content revision. Consumers MUST verify declaration
membership and integrity before staging and materializing those files. Existing
published archives MUST NOT be repaired in place.

#### Scenario: Mirrored hook support installation

- **GIVEN** a mirrored hook whose command references a plugin-root script with adjacent support files
- **WHEN** a consumer resolves and materializes it at repository or machine scope
- **THEN** the support bytes and executable mode MUST be available through the target's native support component or verified support cache, the command MUST refer to that script, and verified cached resolution MUST work without network access

#### Scenario: Mirrored hook support integrity

- **GIVEN** a mirrored hook with declared support files
- **WHEN** a declared file is missing, its bytes or executable mode differ, or an undeclared support file is present
- **THEN** resolution MUST refuse before staging or materializing the hook

#### Scenario: Mirrored hook support safe publication

- **GIVEN** hook support files in a registry repository
- **WHEN** publication reads those files
- **THEN** it MUST use bounded descriptor-based reads within the resource root and refuse symlinks, multiply linked files, special files, and oversized files without altering external bytes

#### Scenario: Mirrored hook support revision changes

- **GIVEN** a published hook with support files
- **WHEN** a support file's bytes or executable mode change
- **THEN** publication MUST produce a new content revision without changing the old artifact

#### Scenario: Legacy mirrored script hook refusal

- **GIVEN** a legacy mirrored hook referencing a plugin-root script without packaged support files
- **WHEN** a consumer resolves the hook
- **THEN** resolution MUST fail with `marketplace.hook.support-missing` instead of reporting a healthy hook
- **AND** legacy support-free hooks without plugin-root asset references MUST remain usable
