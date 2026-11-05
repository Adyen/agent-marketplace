## Why

`list` and the TUI labelled every malformed resource as a skill (#897). A registry whose `commands/foo.md` had invalid frontmatter showed `fixture  skill  foo.md  malformed: …load-command-failed…`, and the TUI showed `✗ skill fixture/foo.md`, contradicting the error text. `list --kind command` also omitted it.

## What Changes

- `skill.MalformedKind` and `skill.MalformedKindLabel` read the kind the loader already records on a malformed entry (`MalformedSkill.Kind`), with `unknown` for unclassified entries.
- `list` labels malformed rows with that kind, so `--kind` filters them correctly.
- TUI malformed rows carry the kind label and, when known, the artifact kind, used by their label and detail.
- SKILL.md-directory discovery records `skill` on its malformed entries. Those are skills, and recording the kind stops them correlating with same-named selections of other kinds.

## Capabilities

### New Capabilities

### Modified Capabilities
- `resource-listing`: malformed resources show their kind.
- `tui-rendering`: malformed rows show their kind.

## Impact

`internal/skill`, `cmd/agent-marketplace` list output, `internal/tui` malformed rows. Malformed rows whose producer did not classify the failure now read `unknown` instead of `skill`.
