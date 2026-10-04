## MODIFIED Requirements

### Requirement: Proven native hooks satisfy legacy output

Before writing a selected hook into a legacy file, a client SHALL recognize the same selected source hook in the harness target's native file when valid durable evidence proves both the native entry's current canonical value and a matching source-hook fingerprint. The evidence SHALL be either current-byte ownership proof or separate native satisfaction evidence that grants no ownership. The files and shapes are fixed for both scopes:

| Harness target | Repository native file | Machine native file | Hook member | Legacy file |
|---|---|---|---|---|
| Claude Code | `.claude/settings.json` | `~/.claude/settings.json` | `hooks` | `hooks/hooks.json` under the target root |
| Cursor | `.cursor/hooks.json` | `~/.cursor/hooks.json` | `hooks` with `version: 1` | `hooks/hooks.json` under the target root |
| Factory Droid | `.factory/hooks.json` | `~/.factory/hooks.json` | top-level event map | `hooks/hooks.json` under the target root |
| Windsurf | `.devin/hooks.json` | `~/.codeium/windsurf/hooks.json` | `hooks` | repository `.windsurf/hooks.json`; machine native file is also the legacy file |

The client SHALL not recreate a legacy entry or report a pending legacy write for a proven match on `sync`, `sync --locked`, or `sync --check`. It SHALL NOT treat a resource name or command string alone as equivalence. A proof for one selected hook SHALL NOT suppress unproven sibling hooks. Every recorded owned or satisfaction entry carrying the selected resource-name and source-fingerprint pair SHALL be present with its recorded current canonical value; partial proof SHALL suppress nothing for that selection. A surviving marked legacy twin SHALL remain byte-identical under this reader-only compatibility behavior.

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
- **AND** a matching native entry without satisfaction evidence MUST retain the ordinary unproven legacy behavior

### Requirement: Source-hook fingerprints are stable across client versions

For source trees with original definition files, the compatibility fingerprint SHALL be `sha256` over a versioned, length-prefixed sequence of byte fields in this order: the literal `hook-source/v1`, stable source identity, installed name, harness target, scope (`repository` or `machine`), source-layout-relative path of the original hook definition, its exact bytes, and a lexically sorted list of regular files in the definition's directory tree other than the definition itself. Each listed file contributes its slash-separated path relative to that directory, permission bits, and exact bytes. Stable source identity SHALL distinguish a registry occurrence or exact Marketplace resource revision; local directories use the canonical configured source identity. Paths SHALL be validated against traversal or symlink escape before reading. If a field is unavailable or the file set cannot be read safely within existing source-size limits, no fingerprint matches.

For a verified Marketplace mirror artifact whose original definition files are unavailable, the fingerprint SHALL instead be `sha256` over a separate length-prefixed sequence: the literal `hook-source/mirror/v1`, Marketplace instance ID, registry ID, resource kind and name, verified resource-revision content digest, installed name, harness target, and scope. The mirror branch MUST use the same verified content digest that pins the artifact; an absent, stale, or unverified digest SHALL NOT establish proof. The two version prefixes SHALL be disjoint so a mirror digest cannot match a source-tree digest. The digest, not payload or support-file content, SHALL be stored with the native hook's owned-entry evidence or separate satisfaction evidence. Both the legacy reader and future native writer SHALL use shared, independently authored golden vectors for each branch.

#### Scenario: Source and support changes invalidate the fingerprint

- **WHEN** a hook's original definition, support script bytes or mode, installed name, source identity, target, or scope changes
- **THEN** the client MUST compute a different version-1 fingerprint and MUST NOT suppress a legacy entry using the prior proof

#### Scenario: Different IR provenance keeps the same fingerprint

- **WHEN** two supported clients ingest the same original source files but represent native event or tool provenance differently in memory
- **THEN** their version-1 fingerprints MUST match the same shared golden vector

#### Scenario: Verified mirror revision uses its own fingerprint

- **WHEN** a selected hook comes from a verified Marketplace mirror resource whose original definition is unavailable, and its native entry has clean ownership proof carrying a fingerprint for that exact resource revision
- **THEN** the legacy-writing client MUST recognize that entry without recreating legacy output
- **AND** the mirror fingerprint MUST differ from any source-tree fingerprint, even if other identity fields match

#### Scenario: Unverified or changed mirror revision never satisfies

- **WHEN** a mirror artifact's content digest is missing, unverified, or differs from the revision recorded with a native entry
- **THEN** the legacy-writing client MUST NOT suppress legacy output or adopt that native entry

### Requirement: Incomplete proof does not suppress legacy output

A native entry without valid current canonical-value evidence and a matching source-hook fingerprint SHALL NOT suppress the existing legacy write. Valid evidence SHALL be current-byte ownership proof or separate source-bound satisfaction proof. The compatibility check SHALL never adopt, change, or prune a handwritten native entry. A changed matcher, event, handler type, command, or relevant execution field SHALL NOT be treated as equivalent merely because its resource name matches.

#### Scenario: Mismatched source or native value retains legacy behavior

- **WHEN** a selected hook has a native entry with a different source fingerprint or changed execution field
- **THEN** the legacy-writing client MUST retain its previous legacy output behavior
- **AND** it MUST NOT change or claim ownership of that native entry

#### Scenario: Handwritten hook does not become owned

- **WHEN** a native hook lacks ownership proof even though it has the same resource name or command as a selected hook
- **THEN** the client MUST NOT treat it as owned, change it, or prune it
- **AND** it MUST NOT suppress its legacy output based solely on that entry

## ADDED Requirements

### Requirement: Native satisfaction evidence does not grant ownership

Separate native satisfaction evidence SHALL identify a native hook's event and canonical value identity and one or more exact resource-name and version-1 source-fingerprint pairs. It SHALL remain distinct from owned-entry evidence. Satisfaction-only entries SHALL NOT become owned entries, receive prune authority, or be returned as owned entries by downstream ownership inspection. Missing satisfaction evidence SHALL require no conversion or implicit state rewrite. Existing ownership-state version 1 and scalar and additional source evidence SHALL remain readable without changing their meaning.

#### Scenario: Satisfaction-only state remains unowned

- **WHEN** version-1 durable state records a native hook only as source-bound satisfaction evidence
- **THEN** ownership inspection MUST classify the entry as unmanaged and report no owned entry for it
- **AND** deselection or owned-entry cleanup MUST NOT delete or adopt the handwritten hook

#### Scenario: Satisfaction proof matches every contributor exactly

- **WHEN** one handwritten native entry has satisfaction evidence for two selected resources
- **THEN** the reader MUST recognize each exact resource-name and source-fingerprint pair without creating duplicate output
- **AND** crossed pairs, an unrecorded resource, and a different fingerprint MUST retain unproven legacy behavior

#### Scenario: Partial mixed evidence suppresses nothing

- **WHEN** a selected resource has matching evidence on several owned or satisfaction-only native entries and any matching entry is missing or has a changed canonical value
- **THEN** the compatibility reader MUST NOT suppress that resource's legacy output using its remaining clean entries

#### Scenario: Old records remain readable without satisfaction fields

- **WHEN** version-1 state contains existing scalar or additional hook source evidence and no satisfaction records
- **THEN** all existing ownership and compatibility decisions MUST remain unchanged
- **AND** inspection MUST NOT rewrite state to add satisfaction records

### Requirement: Satisfaction evidence is validated and read-only

Native satisfaction evidence SHALL carry only non-secret event and resource names and algorithm-qualified SHA-256 digests. It SHALL reject missing source pairs, malformed identities or fingerprints, mismatched native event/value identity, wrong member types, duplicate identities, duplicate source pairs, and ambiguous modeled JSON fields. Invalid evidence SHALL fail closed for the affected target and scope under the existing coded native-tolerance refusal behavior while preserving native, legacy, and state bytes and allowing independent valid targets to reconcile. Diagnostics and logs SHALL NOT contain rejected values or hook payloads. Read-only commands SHALL never create, update, replace, or chmod native, legacy, or state files.

#### Scenario: Invalid satisfaction evidence refuses without mutation

- **WHEN** satisfaction records have invalid fields, duplicate identities or source pairs, or ambiguous modeled JSON fields
- **THEN** the compatibility reader MUST refuse the affected target and scope with a stable coded error without changing native, legacy, or state files
- **AND** independent valid targets MUST continue and diagnostics and logs MUST contain no rejected values

#### Scenario: Satisfaction checks leave all evidence unchanged

- **WHEN** `sync --check` inspects complete, incomplete, or invalid satisfaction proof
- **THEN** native, legacy, and state bytes, modes, and modification times MUST remain unchanged
- **AND** a previously absent state root MUST remain absent
