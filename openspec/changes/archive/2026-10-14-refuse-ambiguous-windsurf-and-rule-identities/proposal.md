## Why

Two ingestion paths picked a silent winner when the same identity appeared twice:

- Windsurf workflows and rules were collected into a map keyed by name, so a later file with the same name replaced an earlier one. A workflow named `deploy` in `zzz.md` replaced the reviewed `deploy.md`, and every target emitted the replacement.
- The tolerant `.mdc` frontmatter parser, used when strict YAML rejects Cursor's unquoted globs, kept the last value of a repeated key. A rule declaring `alwaysApply: false` then `alwaysApply: true` was applied to every file.

## What Changes

- Windsurf ingestion refuses every workflow or rule whose name another file in the same workspace root also claims, with the new error `ingest.windsurf.duplicate-identity`, and keeps unrelated resources.
- The tolerant rule frontmatter parser rejects a repeated key, so the rule fails to load with the strict parser's error.

## Capabilities

### Modified Capabilities

- `harness-compilation`: ambiguous Windsurf identities and rule metadata are refused.

## Impact

Windsurf workspace ingestion and the shared rule loader in `pkg/harness-compilation`, plus one new diagnostic code. Precedence between the user root and the workspace root is unchanged. No fixture executes a resource.
