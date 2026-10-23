## Why

GitHub release bodies contain only pull request titles. Users upgrading across a release have no curated summary, and a narrow breaking change has no place for its upgrade steps, because pull request authors have nowhere to write a user-facing note.

## What Changes

- Every newly added `openspec/changes/**/proposal.md` must carry a `## Release note` section: user-facing sentences, `Breaking: ...` with upgrade steps, or `none — <reason>`. `specaudit gate` enforces this for proposals the diff adds, and never checks pre-existing ones.
- `specaudit release-notes` renders curated Markdown from the release notes of archived proposals added between two refs, grouped into breaking changes and changes, with each entry linked to the pull request that merged it.
- The release job uploads that Markdown as the release body, prepended to GitHub's generated notes, relative to the previous stable tag. `make release-notes` previews it locally.
- The author rule is added to `openspec/config.yaml` and to the pull request template.

## Capabilities

### New Capabilities
- `release-notes`: curated Markdown release notes generated from archived change proposals.

### Modified Capabilities
- `spec-governance`: change proposals carry a release note.

## Impact

`tools/specaudit` (gate rule, `release-notes` command), `.github/workflows/ci.yml` release job, `Makefile`, `.github/pull_request_template.md`, and `openspec/config.yaml`. No runtime CLI behavior changes.

## Release note

Release notes now open with curated summaries of each change, listing breaking changes and their upgrade steps first.
