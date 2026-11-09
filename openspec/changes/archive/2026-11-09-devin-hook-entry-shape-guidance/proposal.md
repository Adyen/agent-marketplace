# Proposal

## Why

When a Devin hooks file has an event entry that does not match Devin's matcher-groups shape, `sync` correctly refuses with `materialize.devin.aggregate-shape` and leaves the file untouched. The printed hint, though, describes a different problem: it says the aggregate root and its `hooks`/`mcpServers` members must be JSON objects, which they already are. Users hitting a stale or hand-written entry are left with no idea what to change.

## What Changes

- An entry-level `materialize.devin.aggregate-shape` refusal, at repository (`.devin/hooks.v1.json`) and user (`~/.config/devin/config.json#hooks`) scope, prints a hint naming the file, event, and entry index, and telling the user to rewrite that entry in the matcher-groups shape or remove it, then run `agent-marketplace sync` again.
- Root and member shape refusals keep the existing cataloged guidance.
- No diagnostic code, severity, message text, or exit status changes. The refused file is still left byte-identical. Entries the CLI cannot prove it wrote are never rewritten or adopted.
- Codex and native-hook (Claude, Gemini, Factory, Cursor, Windsurf) shape refusals are unchanged: their guidance is generic but does not describe a different case.

## Capabilities

### New Capabilities

### Modified Capabilities
- `command-error-output`: "Printed errors name a next step" gains scenarios for Devin hook entry refusals at repository and user scope and for root/member refusals, and the owned-aggregate scenario allows an error's own remediation as its hint.

## Impact

`internal/materialize` (owned-aggregate shape diagnostic construction) and its command tests. No API, configuration, lock, diagnostic catalog, or machine-readable output change.

## Release note

When `sync` refuses a Devin hooks entry that does not match the matcher-groups shape, the hint now names the file, event, and entry index and says how to rewrite or remove that entry.
