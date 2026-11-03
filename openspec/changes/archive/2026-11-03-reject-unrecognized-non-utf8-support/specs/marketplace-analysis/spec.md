## ADDED Requirements

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
