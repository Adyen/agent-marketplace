## Context

The shared analyzer skips text scanners for invalid UTF-8, while inspection
correctly refuses to render those bytes. Asset extension and executable-magic
checks alone do not identify extensionless shell content.

## Goals / Non-Goals

Close the silent analysis gap without rewriting content or weakening inspection.
This is not a general malware detector, binary parser, or encoding converter.
Existing publications require refresh/reassessment to receive the new finding.

## Decisions

- Reject unknown non-UTF-8 support files by default, as selected for #770.
  Warning alone leaves the original installable-content gap; lossy decoding
  introduces another content interpretation and still needs an exclusion policy.
- Use `net/http.DetectContentType` with an explicit allowlist of binary image,
  font, audio/video, PDF, and archive signatures. Filenames and generic
  `application/octet-stream` are not evidence of a recognized format. HTML,
  XML, SVG, PostScript, and executable types are not exempt.
- Emit a file-scoped finding without echoing untrusted content. Apply existing
  policy and finding-retention rules; continue running all asset checks.
- Leave valid UTF-8 scanning and inspection media policy unchanged. The binary
  exemption recognizes signatures, not structural validity or absence of
  polyglot content, and does not attest that a binary is safe to execute.

## Validation

Add failing-before/passing-after analyzer regressions for all resource kinds,
hidden-content variants, deceptive extensions, binary exemptions, and independent
executable detection. Exercise actual registry ingestion in mirror and index-only
modes and verify rejected assessments retain intrinsic content identity and
metadata-only inspection. A declarative E2E case installs a clean control before
proving rejected content cannot change configuration, locks, or harness files.
Run affected package tests, the existing analyzer benchmark, spec-check, make
validate, and the combined E2E gate.
