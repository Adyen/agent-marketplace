## ADDED Requirements

### Requirement: Repository Windsurf portable nominations stay within native output

Repository-scoped Windsurf portable manifest nominations MUST identify descendants of the native `.windsurf/` output namespace. The namespace root itself and unrelated repository paths MUST NOT authorize payload reads, replacement, or pruning through a matching ownership digest. Refused nominations MUST report `materialize.manifest.error`, preserve nominated bytes, identity, permissions, modification time and manifest evidence, and MUST NOT report the refused entry as removed. Genuine Windsurf portable installation and pruning, machine-scope output, and supported initial root and scope aliases MUST remain usable. This requirement does not authenticate manifest provenance, change ownership semantics within authorized output roots, or change SDK explicit-output pruning.

#### Scenario: Windsurf manifest cannot nominate unrelated repository files

- **WHEN** repository file-mode materialization encounters an independently authored Windsurf portable manifest nomination of the native namespace root or an unrelated repository file outside `.windsurf/`, with a matching ownership digest, in a mutating or read-only run
- **THEN** it MUST refuse with `materialize.manifest.error`, preserve the unrelated file bytes, identity, permissions, modification time and manifest evidence, report no removal for that entry, and continue to support actual installation and pruning of native Windsurf portable output
