# hook-native-location-tolerance Specification

## Purpose
Prevent a legacy-writing client from recreating a hook in its old file after a newer client has moved the same selected hook into an ownership-proven native harness file.

## Requirements

### Requirement: Proven native hooks satisfy legacy output

Before writing a selected hook into a legacy file, a client SHALL recognize the same selected source hook in the harness target's native file when ownership state proves both the native entry's current bytes and a matching source-hook fingerprint. The files and shapes are fixed for both scopes:

| Harness target | Repository native file | Machine native file | Hook member | Legacy file |
|---|---|---|---|---|
| Claude Code | `.claude/settings.json` | `~/.claude/settings.json` | `hooks` | `hooks/hooks.json` under the target root |
| Cursor | `.cursor/hooks.json` | `~/.cursor/hooks.json` | `hooks` with `version: 1` | `hooks/hooks.json` under the target root |
| Factory Droid | `.factory/hooks.json` | `~/.factory/hooks.json` | top-level event map | `hooks/hooks.json` under the target root |
| Windsurf | `.devin/hooks.json` | `~/.codeium/windsurf/hooks.json` | `hooks` | repository `.windsurf/hooks.json`; machine native file is also the legacy file |

The client SHALL not recreate a legacy entry or report a pending legacy write for a proven match on `sync`, `sync --locked`, or `sync --check`. It SHALL NOT treat a resource name or command string alone as equivalence. A proof for one selected hook SHALL NOT suppress unproven sibling hooks.

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

### Requirement: Source-hook fingerprints are stable across client versions

For source trees with original definition files, the compatibility fingerprint SHALL be `sha256` over a versioned, length-prefixed sequence of byte fields in this order: the literal `hook-source/v1`, stable source identity, installed name, harness target, scope (`repository` or `machine`), source-layout-relative path of the original hook definition, its exact bytes, and a lexically sorted list of regular files in the definition's directory tree other than the definition itself. Each listed file contributes its slash-separated path relative to that directory, permission bits, and exact bytes. Stable source identity SHALL distinguish a registry occurrence or exact Marketplace resource revision; local directories use the canonical configured source identity. Paths SHALL be validated against traversal or symlink escape before reading. If a field is unavailable or the file set cannot be read safely within existing source-size limits, no fingerprint matches.

For a verified Marketplace mirror artifact whose original definition files are unavailable, the fingerprint SHALL instead be `sha256` over a separate length-prefixed sequence: the literal `hook-source/mirror/v1`, Marketplace instance ID, registry ID, resource kind and name, verified resource-revision content digest, installed name, harness target, and scope. The mirror branch MUST use the same verified content digest that pins the artifact; an absent, stale, or unverified digest SHALL NOT establish proof. The two version prefixes SHALL be disjoint so a mirror digest cannot match a source-tree digest. The digest, not payload or support-file content, SHALL be stored with the native hook's ownership entry. Both the legacy reader and future native writer SHALL use shared, independently authored golden vectors for each branch.

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

A native entry without both valid current-byte ownership proof and a matching source-hook fingerprint SHALL NOT suppress the existing legacy write. The compatibility check SHALL never adopt, change, or prune a handwritten native entry. A changed matcher, event, handler type, command, or relevant execution field SHALL NOT be treated as equivalent merely because its resource name matches.

#### Scenario: Mismatched source or native value retains legacy behavior

- **WHEN** a selected hook has a native entry with a different source fingerprint or changed execution field
- **THEN** the legacy-writing client MUST retain its previous legacy output behavior
- **AND** it MUST NOT change or claim ownership of that native entry

#### Scenario: Handwritten hook does not become owned

- **WHEN** a native hook lacks ownership proof even though it has the same resource name or command as a selected hook
- **THEN** the client MUST NOT treat it as owned, change it, or prune it
- **AND** it MUST NOT suppress its legacy output based solely on that entry

### Requirement: Unsafe native input is refused without mutation

When a native hook file or its ownership evidence is malformed, ambiguous, or outside the authorized root, the compatibility check SHALL fail closed for the affected target and scope with `hook.native-tolerance-refused` (or `json.duplicate-key` for a duplicate JSON key). It SHALL preserve that target's existing native and legacy bytes. Independent targets SHALL continue. Read-only commands SHALL NOT write files or ownership state.

#### Scenario: Malformed native file leaves both locations unchanged

- **WHEN** a target's native hook file contains duplicate keys, a wrong hook envelope, or an unsafe file path and `sync` runs
- **THEN** that target's native and legacy file bytes MUST remain unchanged and a coded error MUST identify the refusal
- **AND** a separately selected valid target MUST still reconcile

#### Scenario: Read-only check cannot mutate ownership state

- **WHEN** `sync --check` examines a proven native hook or an invalid native hook file
- **THEN** it MUST NOT create or update a native file, legacy file, or ownership-state file

### Requirement: Shared native entries retain proof for every contributing source

Ownership state for one owned native hook entry SHALL be able to record source-hook evidence for several selected resources that produce that entry. Each evidence tuple SHALL contain only a resource name and its version-1 source-hook fingerprint. Existing scalar resource-name and source-fingerprint evidence SHALL remain readable under the existing ownership-state version.

A legacy-writing client SHALL recognize a selected hook when its resource name and fingerprint match valid scalar evidence or a recorded evidence tuple and every matching owned native entry is present with clean current-byte proof. A matching tuple SHALL NOT establish current-byte ownership by itself or suppress a different, unproven resource, target, scope, or source revision. Missing or drifted matching entries SHALL suppress nothing for the affected selection.

#### Scenario: Several selected sources share one clean native entry

- **WHEN** two selected hook resources produce one native entry and clean ownership state records valid source-hook evidence for both resources
- **THEN** `sync`, `sync --locked`, and `sync --check` MUST recognize both resources without recreating legacy entries or reporting pending legacy writes
- **AND** they MUST leave the proven native entry and its ownership state unchanged

#### Scenario: Scalar source evidence remains compatible

- **WHEN** ownership state has a valid scalar resource name and source-hook fingerprint with no additional evidence tuples
- **THEN** the legacy-writing client MUST recognize that resource exactly as before
- **AND** it MUST NOT rewrite ownership state to add tuples merely by reading it

#### Scenario: Unproven contributor retains legacy behavior

- **WHEN** a shared native entry proves one selected resource but another selected resource has no matching resource-name and source-fingerprint tuple
- **THEN** the client MUST retain normal legacy output behavior for the unproven resource
- **AND** it MUST NOT change or adopt the native entry

#### Scenario: Partial shared proof cannot suppress a selection

- **WHEN** one selected resource has matching evidence on several recorded native entries and any matching entry is missing or drifted
- **THEN** the client MUST NOT suppress that resource's legacy output based on the remaining clean entries

### Requirement: Additional source evidence is validated and read-only

Additional source evidence SHALL be valid only on hook ownership entries. Resource names and source fingerprints SHALL satisfy the same non-secret validation rules as scalar evidence; empty names, absent or malformed fingerprints, wrong member types, and ambiguous modeled JSON fields SHALL NOT establish proof. Invalid ownership evidence SHALL fail closed for the affected target and scope under the existing native-tolerance refusal behavior, preserve native and legacy bytes, and leave independent valid targets able to continue. Diagnostics and logs SHALL NOT include rejected values or hook payloads.

#### Scenario: Invalid additional evidence refuses without mutation

- **WHEN** a hook's additional source evidence is malformed or ambiguous, or an MCP server ownership entry carries hook source evidence
- **THEN** the compatibility reader MUST refuse the affected target and scope with a stable coded error without changing its native file, legacy file, or ownership state
- **AND** the diagnostic and logs MUST NOT include the rejected values

#### Scenario: Read-only shared-proof checks leave state untouched

- **WHEN** `sync --check` checks complete or incomplete source evidence for a shared native hook entry
- **THEN** it MUST NOT create, update, chmod, or replace a native file, legacy file, or ownership-state file
