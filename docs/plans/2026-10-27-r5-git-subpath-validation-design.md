# R5 / R53: Git Subpath Validation

## Scope And Choice

Independent partial R53/F136 progress from main `47be33f6`, after #963.
The remaining split in `validateV4Subpath` constructs a slice only to
iterate over its segments. Prefer a one-line `strings.SplitSeq` change
over a handwritten Cut loop or retaining unnecessary materialization.
The module already requires Go 1.27.3; add no dependency or helper.

This is not an admission budget, new syntax, or a relaxation of path
safety. Keep NUL/absolute checks, backslash normalization, `path.Clean`,
escape refusal, segment validation, and diagnostic order unchanged.

## Boundary

The only direct caller is the Git branch of `parseV4Source`. It first
validates source fields, Git ref, and origin, validates the authored
trimmed subpath, then normalizes it for source identity. Directory and
Marketplace declarations retain their existing path-field refusals.

Trace public parsing and recursive file loading, not just the helper.
Neither path acquires Git content. Preserve `v4.git-path`, source/field
identity, file and line context, normalized valid paths, source-bound
requests, and unchanged input file bytes/modes/mtimes/directory contents.
Do not add a new precheck, move validation, or change path publication.

## Measurement

Add ten direct validator shapes and nine public parsing cases before
changing production. Public cases have 1, 256, and 1,024 Git sources,
each with one bound skill, using empty/nested/backslash paths.
Empty paths and early absolute/escape refusals are unaffected controls.

Freeze definitions after baseline preservation tests and lint. Capture
an actual repeated before trial before the production edit. Compile
separate native Go 1.27.3 binaries and run six alternating one-second
pairs across all nineteen cases, retaining all 228 observations.
Use `b.Loop`, allocation reports, result assertions outside timing,
source/binary/trial SHA-256, and byte-exact benchstat output.

Profile baseline allocation sites. The direct helper has no filesystem
or network work. Public timing includes YAML, Git URL/ref validation,
and normalization, but no acquisition, config reads, or directory path
metadata. Do not infer CLI/acquisition latency or pool different bases.

## Preservation And Review

Independently author path acceptance and refusal-priority examples.
Freeze the former split-based helper as a raw-byte fuzz replay oracle;
shared path-cleaning routines remain unchanged. Public parsing and
actual file loading prove typed code/path/file/line and diagnostic
message preservation, including backslash traversal and empty segments.
Use semantic mutants to establish public observability.

Run before/after tests, native fuzzing, focused races, offline module
hygiene, selected validation, strict specs, terminal QA, and the combined
E2E/refusal-witness gate. A scoped package run is not full-suite proof.

Host adversarial `open-code-review-delegate` covers all own files,
including default-excluded tests/docs/raw data and private harnesses,
before every push and handoff. Follow actionable CodeRabbit and Gemini
feedback. Ignore major-release commentary, not compatibility defects.
Leave the clean PR open, unqueued, with auto-merge disabled. R53, R5,
and #765 remain partial.
