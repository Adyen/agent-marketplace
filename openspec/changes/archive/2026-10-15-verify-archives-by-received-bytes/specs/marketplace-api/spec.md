## ADDED Requirements

### Requirement: Archive digests verify by received bytes

The Marketplace and its clients SHALL verify canonical archive digests by
hashing bytes they hold, without reproducing a compressor's output. A
placeholder-form archive is verified by hashing its bytes against the digest.
A final archive is verified by content equality with the hash-verified
placeholder form. The re-encoding verifier remains only as a fallback for
archives that have no placeholder form.

#### Scenario: Placeholder archive verifies regardless of the encoding toolchain

- **GIVEN** placeholder-form archives recorded by Go 1.26 and by Go 1.27 with
  different digests but identical content
- **WHEN** each is verified by its digest
- **THEN** verification MUST succeed under either toolchain
- **AND** a final archive from the same toolchain MUST verify against its
  placeholder by content equality

#### Scenario: Final archive with different bytes but identical content verifies

- **GIVEN** a verified placeholder-form archive and a final archive whose
  bytes differ from the encoder output but decode to the same content
- **WHEN** the final archive is verified against the placeholder
- **THEN** verification MUST succeed
- **AND** a final archive whose payload, asset, license, or support content
  differs MUST fail verification

#### Scenario: Placeholder archive route serves archive-digest bytes

- **GIVEN** a resource revision owned by the Marketplace with a stored or
  derivable placeholder-form archive
- **WHEN** `GET /api/v1/archives/sha256/{hex}` is requested for its archive digest
- **THEN** the response MUST carry bytes whose SHA-256 equals the requested digest
- **AND** the response MUST use the artifact media type
- **AND** an unowned digest MUST return 404, a revoked revision MUST return 410, and a blocked revision MUST return 403

#### Scenario: Missing placeholder archive is derived only when reproducible

- **GIVEN** a stored final archive with no stored placeholder-form archive
- **WHEN** the placeholder route is requested for its archive digest
- **THEN** the Marketplace MUST derive the placeholder archive from the final archive and store it when the derived bytes hash to the digest
- **AND** when the derived bytes do not hash to the digest the response MUST be 404 with a placeholder-archive-unavailable message

#### Scenario: Artifact route serves stored final archives without re-compression

- **GIVEN** a stored final archive recorded by a different Go toolchain whose placeholder-form archive is stored
- **WHEN** the artifact route is requested for its archive digest
- **THEN** the response MUST serve the stored final bytes
- **AND** the response MUST NOT fail with a digest mismatch caused by the server's compressor

#### Scenario: Worker backfills placeholder archives

- **GIVEN** stored resource revisions with archive digests and no placeholder-form archive
- **WHEN** the worker runs the backfill pass or `serve reindex` completes
- **THEN** each reproducible placeholder-form archive MUST be stored under its placeholder key
- **AND** unreproducible archives MUST be counted and reported without failing the pass or readiness

### Requirement: Placeholder archive route is served in the OpenAPI contract

The OpenAPI contract SHALL document `GET /api/v1/archives/sha256/{hex}`
with the artifact media type and the same availability responses as the artifact route.

#### Scenario: Placeholder archive route is documented

- **WHEN** the OpenAPI contract is loaded
- **THEN** it MUST define the placeholder-archive route with the artifact media type
- **AND** it MUST document 403, 404, 410, and 500 responses

### Requirement: Clients verify archives by received bytes

A client SHALL verify a resource archive by hashing the placeholder-form archive
it receives from `GET /api/v1/archives/sha256/{hex}`, so that locks
and caches written under any Go toolchain verify under any other. A client SHALL
fall back to the final artifact route only when the placeholder route answers
404, and it SHALL treat every other failure from the placeholder route as
terminal. The single remaining re-encoding check applies only to a final archive
without a placeholder form, and its failure SHALL be reported as an encoding
mismatch rather than as tampering, unless the decoded content itself fails
verification.

#### Scenario: Client verifies placeholder archives by hash

- **WHEN** a client resolves a mirrored resource whose archive was published by a different Go toolchain
- **THEN** the client MUST fetch the placeholder-form archive and verify it by hashing its bytes against the locked archive digest
- **AND** the client MUST NOT re-compress the archive to verify it

#### Scenario: Locks written under any toolchain stay valid

- **GIVEN** a lock whose archive digest was recorded by one Go toolchain
- **WHEN** a client built with another Go toolchain resolves, syncs, or checks that lock
- **THEN** the lock MUST verify without changing its archive digest
- **AND** the resource content MUST verify against its content digest

#### Scenario: Fallback to the artifact route only on not found

- **WHEN** the placeholder route answers 404 for an archive digest
- **THEN** the client MUST request the final artifact route for that digest
- **AND** when the placeholder route answers 410, 403, a server error, or a network failure, the client MUST fail with that error without requesting the artifact route

#### Scenario: Unreproducible legacy archive reports an encoding mismatch

- **GIVEN** a Marketplace that does not serve the placeholder form for an archive
- **WHEN** the archive it serves was compressed by an encoder that this build does not reproduce
- **THEN** the client MUST fail with an encoding-mismatch diagnostic that names the resource, the Marketplace, the expected and re-encoded digests, the agent-marketplace Go version, and the remediation to upgrade the Marketplace and run sync
- **AND** a served archive whose decoded content fails verification MUST remain an integrity failure rather than an encoding mismatch

#### Scenario: Cached unreproducible archive is refetched online

- **GIVEN** a cached final archive that this build does not reproduce
- **WHEN** a refresh resolves the resource online
- **THEN** the client MUST discard the cached copy, fetch the archive once, verify the fetched bytes, and cache them in place of the stale copy
- **AND** when the resolution is offline the client MUST fail with the cached-copy diagnostic that asks for `agent-marketplace sync` while online

#### Scenario: Index-only resolution verifies the registry revision manifest by hash

- **WHEN** an index-only resource is resolved from an exact registry occurrence
- **THEN** the client MUST verify that the registry revision manifest payload hashes to the occurrence's manifest digest before using it
- **AND** the locally rebuilt resources MUST match the manifest's kind, name, content digest, and support-file digests
- **AND** a manifest whose payload does not hash to the occurrence digest, or whose resource entries differ from the rebuilt resources, MUST fail resolution

#### Scenario: Received archive bytes verify across toolchains

- **GIVEN** placeholder-form and final archives recorded by Go 1.26 and by Go 1.27
- **WHEN** each placeholder-form archive is verified by hashing its bytes against its digest under either toolchain
- **THEN** verification MUST succeed
- **AND** a final archive recorded by the other toolchain and verified without its placeholder form, as after a 404 from the placeholder route, MUST fail with an encoding mismatch, while tampered final content MUST fail as an integrity error

### Requirement: Placeholder-form archives are cached apart from legacy artifacts

A client SHALL cache placeholder-form archives under a location separate from the
legacy artifact path, so that a client built before this change, which validates
whatever it finds at the legacy path, never reads placeholder bytes there. Final-form
archives SHALL be written to the legacy path only when the legacy artifact route was
used. The garbage collector SHALL treat both locations with the same reachability
rules, and SHALL preserve entries it does not recognize.

#### Scenario: Placeholder-form archives stay out of the legacy artifact path

- **WHEN** a client fetches a placeholder-form archive for a locked digest
- **THEN** the client MUST store the placeholder bytes only under the placeholder location
- **AND** the legacy artifact path MUST NOT receive placeholder bytes, so a client that predates this change finds nothing there to validate or refetches

#### Scenario: Cached placeholder-form archive serves offline resolution

- **GIVEN** a verified placeholder-form archive in the cache
- **WHEN** a resolution runs in cached or cached-no-fetch mode
- **THEN** the client MUST resolve the resource from the placeholder location without contacting the Marketplace

#### Scenario: Placeholder-form archive is collected with its archive

- **WHEN** garbage collection runs and a digest is unreferenced
- **THEN** it MUST remove the placeholder-form and legacy entries for that digest together
- **AND** it MUST keep both entries for a referenced digest, and MUST preserve malformed or unrecognized placeholder entries and fail closed on symlinked ones

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
  supplied without a verified placeholder-form archive, it MUST fail even if
  the manifest digest is correct
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
