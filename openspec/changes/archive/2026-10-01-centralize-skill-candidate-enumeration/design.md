## Context

See proposal.md for motivation. Five source layouts enumerate skill subdirectories with local helpers; Codex enumerates entries inline. Factory additionally resolves directory symlinks.

## Goals / Non-Goals

Keep candidate exclusion shared while leaving source-layout parsing, malformed-skill handling, and symlink policy local. Do not filter other resource trees or change explicit source roots.

## Decisions

Use `ingest.ReadSkillEntries(fs.FS, root)` to read sorted entries and remove only the literal `.git` name before callers inspect entry types or resolve symlinks. Preserve other entries and read-error identity. This avoids importing Git metadata policy into each source layout.

A global filesystem wrapper would widen the policy beyond skill enumeration. Filtering diagnostics afterward misses direct ingester callers and cannot prevent Windsurf's bundle error. Requiring `SKILL.md` before admitting candidates would hide malformed authored skills.

## Risks / Trade-offs

Factory's ordinary skill symlinks must keep working. Keep its resolution branch unchanged and test it alongside the excluded metadata symlink. Near-match names and sorted entries remain discoverable. Existing cross-layout and consumer regressions guard diagnostics and failure behavior; before/after ingestion benchmarks measure the helper's cost without a startup-performance claim.
