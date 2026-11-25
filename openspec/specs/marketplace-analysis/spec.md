# marketplace-analysis Specification

## Purpose
Defines deterministic marketplace content findings and source-location fidelity while preserving existing detection and processing policies.

## Requirements

### Requirement: Ordered text finding ranges retain rune-location fidelity

Marketplace Analysis MUST retain detection identity, finding count and ordering, source file and analyzer, and one-based rune line/column locations when avoiding repeated prefix conversion for ordered findings. Reused range-conversion state MUST be invocation-local, and invalid standalone offsets MUST retain a file-only range. This requirement adds no input, finding or graph-work budget and does not alter detection rules.

#### Scenario: Dense comment findings retain ordered rune locations

- **WHEN** bounded inert text contains an ordered HTML comment on each line after a non-ASCII rune
- **THEN** public analysis MUST emit one HTML-comment finding per line in original order, at column two with unchanged file/analyzer identity

### Requirement: Non-UTF-8 support files cannot silently bypass analysis

Marketplace Analysis MUST emit `marketplace.find.invalid-utf8` for a support
file whose bytes are not valid UTF-8 unless the bytes match an explicitly
recognized binary signature. The public policy MUST reject that finding.
This rule MUST preserve original content bytes and existing inspection body
restrictions.

#### Scenario: Unknown non-UTF-8 support files receive a rejecting finding

- **WHEN** a support file contains invalid UTF-8 without a recognized binary
  signature, including a file with concealed terminal controls, bidi reordering,
  or encoded instructions, or a misleading binary filename extension
- **THEN** the shared analyzer MUST emit the file-scoped invalid-UTF-8 finding
  and reject the resource under public policy, for every analyzed resource kind

#### Scenario: Ingest persists rejection beside unavailable inspection content

- **WHEN** mirror or index-only ingestion processes a resource with an unknown
  non-UTF-8 support file that inspection classifies as metadata-only
- **THEN** its assessment MUST reject with the invalid-UTF-8 finding rather than
  silently accepting unscanned, unviewable content
- **AND** the intrinsic resource identity and original support bytes MUST remain
  unchanged

#### Scenario: Rejected non-UTF-8 support cannot be installed

- **GIVEN** a refreshed Marketplace registry containing a clean resource and a
  resource with unknown non-UTF-8 support
- **WHEN** a client installs the clean resource and then attempts the rejected
  resource
- **THEN** the clean installation MUST succeed and the rejected installation
  MUST refuse without changing configuration, locks, or harness files

### Requirement: Binary signature exemptions remain narrow

The binary exemption MUST use the recognized formats enumerated below, not
filename extensions or generic `application/octet-stream`. It MUST NOT include
active text formats or executable signatures. Independent asset checks MUST
still run.

#### Scenario: Recognized binaries retain their text scan exemption

- **GIVEN** a binary image (ICO, BMP, GIF, WebP, PNG, JPEG), font (EOT, TTF, OTF,
  TTC, WOFF, WOFF2), audio/video (AIFF, MP3, Ogg, MIDI, AVI, WAV, MP4, WebM), PDF,
  or archive (gzip, ZIP, RAR) signature
- **WHEN** non-UTF-8 support matches that signature and contains bytes
  resembling text findings
- **THEN** analysis MUST NOT emit text or invalid-UTF-8 findings for those bytes
- **AND** independent executable-extension and path checks MUST still apply

### Requirement: Configured analyzers report independently

Marketplace publication MUST retain the built-in security findings and report the result of each enabled supplementary skill analyzer separately for the exact skill revision. Supplementary findings MUST NOT influence the Marketplace policy decision or change intrinsic resource, archive, or inspection projection identity.

#### Scenario: Both tools report for mirrored and index-only skills

- **WHEN** Marketplace publishes a skill with SkillSpector enabled and both tools finish
- **THEN** the resource assessment MUST expose separately attributed built-in and SkillSpector results in both acquisition modes
- **AND** the existing policy decision and intrinsic digests MUST match publication with only the built-in analyzer

#### Scenario: Supplementary analysis cannot weaken rejection

- **WHEN** built-in analysis rejects a skill and the supplementary analyzer reports no findings
- **THEN** the resource assessment MUST remain rejected and distribution MUST continue to follow the existing reject policy

### Requirement: Incomplete analysis is distinct from no findings

Supplementary reports MUST distinguish complete, complete with caveats, partial, failed, and not-run analysis. A supplementary timeout, invalid result, unavailable runner, or exhausted scan budget MUST NOT prevent otherwise valid publication or appear as a scan with no findings.

#### Scenario: Partial report preserves supported findings

- **WHEN** a supplementary scanner completes with useful findings and explicit coverage gaps
- **THEN** its report MUST retain bounded findings and identify the coverage gap without changing the built-in result

#### Scenario: Supplementary scanner fails or cannot start

- **WHEN** an attempted supplementary scan times out, is unavailable, or returns invalid output, or an enabled scan cannot start within its aggregate budget
- **THEN** Marketplace MUST publish the untouched built-in outcome and distinguish a failed attempt from work not run
