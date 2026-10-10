## MODIFIED Requirements

### Requirement: Proven native hooks satisfy legacy output

Before writing a selected hook into a legacy file, a client SHALL recognize the same selected source hook in the harness target's native file when valid durable evidence proves both the native entry's current canonical value and a matching source-hook fingerprint, or when complete repository-scoped independent native satisfaction is established under the source-derived satisfaction requirements. Durable evidence SHALL be either current-byte ownership proof or separate native satisfaction evidence that grants no ownership. Independent satisfaction SHALL be ephemeral, source-derived and non-owning and SHALL NOT substitute for corrupt, contradictory, drifted or partial private evidence. The files and shapes are fixed for both scopes:

| Harness target | Repository native file | Machine native file | Hook member | Legacy file |
|---|---|---|---|---|
| Claude Code | `.claude/settings.json` | `~/.claude/settings.json` | `hooks` | `hooks/hooks.json` under the target root |
| Cursor | `.cursor/hooks.json` | `~/.cursor/hooks.json` | `hooks` with `version: 1` | `hooks/hooks.json` under the target root |
| Factory Droid | `.factory/hooks.json` | `~/.factory/hooks.json` | top-level event map | `hooks/hooks.json` under the target root |
| Windsurf | `.devin/hooks.json` | `~/.codeium/windsurf/hooks.json` | `hooks` | repository `.windsurf/hooks.json`; machine native file is also the legacy file |

The client SHALL not recreate a legacy entry or report a pending legacy write for a proven match on `sync`, `sync --locked`, or `sync --check`. It SHALL NOT treat a resource name or command string alone as equivalence. A proof for one selected hook SHALL NOT suppress unproven sibling hooks. Every recorded owned or satisfaction entry carrying the selected resource-name and source-fingerprint pair SHALL be present with its recorded current canonical value; partial proof SHALL suppress nothing for that selection. A surviving marked legacy twin SHALL remain byte-identical under this reader-only compatibility behavior. Independent reconstruction SHALL NOT grant authority to remove such a twin or modify a native entry.

#### Scenario: Native owned entry prevents duplicate legacy output

- **WHEN** a selected hook was moved into the target's native hook file, its current bytes have ownership proof, and its source-hook fingerprint matches the selected hook in both scopes for each of Claude Code, Cursor, Factory Droid, and Windsurf
- **THEN** `sync` MUST not recreate that hook in a legacy file or report drift
- **AND** `sync --locked` and `sync --check` MUST succeed without reporting a pending legacy entry or modifying any file

#### Scenario: Native entry with reordered JSON keys still satisfies

- **WHEN** a proven native hook's JSON object has equivalent fields in a different key order
- **THEN** the legacy-writing client MUST recognize the same hook without rewriting the native or legacy files

#### Scenario: Windsurf machine hooks share one file

- **WHEN** a proven native hook and a handwritten or marker-owned legacy sibling coexist in the machine-scope `~/.codeium/windsurf/hooks.json`
- **THEN** the client MUST not duplicate the proven hook in that file
- **AND** it MUST keep the unrelated sibling's bytes and order and continue to reconcile unproven selected hooks normally

#### Scenario: Handwritten native satisfaction prevents legacy recreation

- **WHEN** separate durable satisfaction evidence binds an unchanged handwritten native hook to the selected resource and its exact version-1 source fingerprint for Claude Code, Cursor, Factory Droid, or Windsurf in either scope
- **THEN** `sync`, `sync --locked`, and `sync --check` MUST recognize the selected hook without recreating a legacy entry or reporting a pending legacy write
- **AND** the native entry MUST remain unmanaged and its bytes, mode, and modification time MUST remain unchanged
- **AND** a matching native entry without durable satisfaction evidence or complete source-derived repository satisfaction MUST retain the ordinary unproven legacy behavior

### Requirement: Incomplete proof does not suppress legacy output

A native entry without valid current canonical-value evidence and a matching source-hook fingerprint, or without complete independently reconstructed repository satisfaction, SHALL NOT suppress the existing legacy write. Durable evidence SHALL be current-byte ownership proof or separate source-bound satisfaction proof. Source-derived satisfaction SHALL verify the complete expected native entries and portable support from verified selected input and SHALL NOT be established by payload similarity alone. The compatibility check SHALL never adopt, change, or prune a handwritten native entry. A changed matcher, event, handler type, command, or relevant execution field SHALL NOT be treated as equivalent merely because its resource name matches. Existing partial or drifted private evidence for the selection SHALL NOT be bypassed by reconstruction.

#### Scenario: Mismatched source or native value retains legacy behavior

- **WHEN** a selected hook has a native entry with a different source fingerprint or changed execution field
- **THEN** the legacy-writing client MUST retain its previous legacy output behavior
- **AND** it MUST NOT change or claim ownership of that native entry

#### Scenario: Handwritten hook does not become owned

- **WHEN** a native hook lacks ownership proof even though it has the same resource name or command as a selected hook
- **THEN** the client MUST NOT treat it as owned, change it, or prune it
- **AND** it MUST NOT suppress its legacy output based solely on that entry without complete independent source-derived satisfaction
