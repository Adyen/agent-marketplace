## Why

Consumers can pin resource revisions, but cannot review the resource content an
update would introduce before changing their acquisition locks and installation.
Revision identifiers alone do not reveal changed instructions, supporting
scripts, hook commands, or MCP server definitions.

## Release note

Preview updates without changing your configuration, locks, or installed resources
using `update --dry-run`. Add `--include-diff` to review normalized selected-resource
definitions and exact supporting-file changes, either as a preview or alongside an
applying update.

## What Changes

- Add `update --dry-run` to report proposed dependency and selected-resource
  changes without publishing configuration or acquisition locks, materializing
  resources, replaying pending transactions, or changing installation state.
- Add an independent `--include-diff` flag. With `--dry-run`, it shows proposed
  changes; without it, update applies the prepared candidates and reports the
  corresponding changes without an approval prompt.
- Compare the previously locked and candidate selected-resource sets for Git
  sources and Marketplace installations. Cover skills, commands, subagents,
  hooks, MCP servers, and selected collections, including their supporting files.
- Use readable, normalized, harness-independent definitions and exact admitted
  supporting-file bytes. The review does not promise original authored
  formatting, comments, a patch that can be applied, or generated harness files.
- Identify content additions, modifications, removals, source changes, executable
  mode changes, and binary identity changes. Separate revision/evidence-only
  changes from changes to reviewed content.
- Preserve existing selection, scope, targeting, policy, source-containment,
  partial-update, and recovery behavior. An unavailable required comparison must
  fail before new update publication when `--include-diff` is requested.
- Show authored content only in the explicitly requested diff output. Never
  expand local secrets or insert acquisition credentials, and keep content out
  of diagnostics, operational logs, and telemetry.

No TUI interactions, interactive approval, saved plans, local-edit merging,
rollback, or changes to Marketplace security-findings policy are included.

## Capabilities

### New Capabilities

- `update-preview`: non-mutating update preview, command composition, state
  preservation, refusal behavior, and limits of the preview.

### Modified Capabilities

- `update-output`: optional normalized resource diffs, comparison completeness,
  truthful proposed/applied reporting, and an explicit distinction between
  applying and previewing updates when Marketplace resources are missing.

## Impact

- CLI flags, help, update reports, and successful behavioral command witnesses.
- Shared update and dependency preparation: expose candidate state before
  publication, reuse it for preview/apply, and retain exact acquisition evidence.
- Resource comparison and rendering for both Git and Marketplace acquisition,
  using existing canonical resource and supporting-file information without
  changing any acquisition-lock, artifact, source-revision, or wire schema.
- CLI reference and the explanation of how upstream changes reach consumers.
- Scenario-bound package tests and isolated v4 E2E witnesses, completed with the
  implementation and archived spec deltas in this pull request.
