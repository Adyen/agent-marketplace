## Purpose

Prevent a legacy-writing client from recreating a hook in its old file after a newer client has moved the same selected hook into an ownership-proven native harness file.

## ADDED Requirements

### Requirement: Proven native hooks satisfy legacy output

Before writing a selected hook into a legacy file, a client SHALL recognize the same selected source hook in the harness target's native file when ownership state proves both the native entry's current bytes and a matching source-hook fingerprint. This applies to Claude Code, Cursor, Factory Droid, and Windsurf in repository and machine scope. The client SHALL not recreate a legacy entry or report a pending legacy write for a proven match on `sync`, `sync --locked`, or `sync --check`. It SHALL NOT treat a resource name or command string alone as equivalence.

#### Scenario: Native owned entry prevents duplicate legacy output

- **WHEN** a selected hook was moved into the target's native hook file, its current bytes have ownership proof, and its source-hook fingerprint matches the selected hook in both scopes for each of Claude Code, Cursor, Factory Droid, and Windsurf
- **THEN** `sync` MUST not recreate that hook in a legacy file or report drift
- **AND** `sync --locked` and `sync --check` MUST succeed without reporting a pending legacy entry or modifying any file

#### Scenario: Native entry with reordered JSON keys still satisfies

- **WHEN** a proven native hook's JSON object has equivalent fields in a different key order
- **THEN** the legacy-writing client MUST recognize the same hook without rewriting the native or legacy files

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

When a native hook file or its ownership evidence is malformed, ambiguous, or outside the authorized root, the compatibility check SHALL fail closed for the affected target and scope with a coded diagnostic. It SHALL preserve that target's existing native and legacy bytes. Independent targets SHALL continue. Read-only commands SHALL NOT write files or ownership state.

#### Scenario: Malformed native file leaves both locations unchanged

- **WHEN** a target's native hook file contains duplicate keys, a wrong hook envelope, or an unsafe file path and `sync` runs
- **THEN** that target's native and legacy file bytes MUST remain unchanged and a coded error MUST identify the refusal
- **AND** a separately selected valid target MUST still reconcile

#### Scenario: Read-only check cannot mutate ownership state

- **WHEN** `sync --check` examines a proven native hook or an invalid native hook file
- **THEN** it MUST NOT create or update a native file, legacy file, or ownership-state file
