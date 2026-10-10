## Purpose

Keep independent clients from duplicating selected repository hooks when complete source-derived native satisfaction can be verified without sharing ownership state or granting owned-entry authority.

## ADDED Requirements

### Requirement: Verified selected sources define complete native satisfaction

For repository scope, a compatibility reader SHALL independently derive the expected native entries and portable support for the exact selected hook from its verified source input and shipped harness-target catalogs. It SHALL require complete canonical native-value equivalence, including event, matcher, handler type, command or prompt, timeout, environment references and meaningful execution fields. A resource name, command string, unsigned claim or another client's ownership state alone SHALL NOT establish satisfaction. The source identity, installed name, harness target and scope SHALL participate in the derivation. Existing source-revision generators and hook-fingerprint domains SHALL retain their meanings.

#### Scenario: Independent clients recognize complete repository projection

- **WHEN** a candidate client publishes the complete expected native entries for a selected repository hook and another client with an independent HOME and ownership state runs `sync --locked` and `sync --check` against the same acquisition lock
- **THEN** both commands MUST succeed without recreating legacy output for that hook
- **AND** the healthy acquisition lock and native file bytes, permissions and modification time MUST remain unchanged

#### Scenario: Changed execution fields do not establish satisfaction

- **WHEN** a native entry differs from the selected hook's complete expected projection in its matcher, event, handler type, command, prompt, timeout, environment references or meaningful extra execution fields
- **THEN** the client MUST NOT suppress legacy output based on that entry
- **AND** it MUST NOT modify or claim ownership of the native entry

#### Scenario: Partial handler coverage suppresses nothing

- **WHEN** a selected resource projects several native handlers and any expected handler is missing or changed
- **THEN** the client MUST NOT suppress legacy output for that selected resource using its remaining matching handlers

#### Scenario: Target scope and resource derivations remain isolated

- **WHEN** an entry matches another resource's derivation, another harness target or machine scope instead of the selected repository hook
- **THEN** it MUST NOT establish repository satisfaction for the selected resource
- **AND** independent valid resources and targets MUST continue normally

### Requirement: Portable support is verified without private-path authority

Source-derived repository satisfaction SHALL require every expected support file at the declared per-source and installed-name namespace as regular-file content with exact verified bytes and permission bits. Reads SHALL use the existing authorized-root, descriptor-anchored, regular-file, link-count, byte-bound and generation safeguards. Missing, changed, unstable or unsafe support SHALL NOT establish satisfaction. The reader SHALL NOT follow arbitrary hook-command paths or another client's HOME or private cache links.

#### Scenario: Complete portable support establishes satisfaction

- **WHEN** all expected native entries and regular support copies match the verified selected source at their declared repository namespace
- **THEN** the compatibility reader MUST recognize complete native satisfaction without reading another client's private paths or publishing support files

#### Scenario: Support bytes and permissions participate in the match

- **WHEN** an expected support file is missing or its bytes or permission bits differ from the verified selected source
- **THEN** the client MUST NOT suppress legacy output for that selection based on incomplete support
- **AND** it MUST preserve native and installed support bytes and permissions

#### Scenario: Unsafe support refuses the affected target without mutation

- **WHEN** an expected support path has a symlink escape, hard link, special file, oversized input or unstable generation
- **THEN** the affected target and scope MUST refuse safely with `hook.native-tolerance-refused` without changing native, legacy, support or ownership-state files
- **AND** independent valid targets MUST continue and diagnostics and logs MUST not contain rejected values or hook payloads

### Requirement: Source-derived satisfaction is non-owning and read-only

Source-derived satisfaction SHALL be ephemeral and distinct from ownership state. It SHALL NOT create an owned entry, authorize adoption or pruning, modify existing ownership evidence, grant harness trust or change ownership-state version meanings. Existing corrupt, contradictory, drifted or incomplete private proof for a selection SHALL retain its refusal or unproven behavior and SHALL NOT be bypassed by reconstruction. A surviving marked legacy twin SHALL remain protected by existing reader-only preservation behavior. Machine-scope behavior and other resource kinds SHALL remain unchanged.

#### Scenario: Reconstruction grants no ownership or pruning

- **WHEN** an unmanaged native entry exactly satisfies a selected hook by independent reconstruction
- **THEN** the entry MUST remain unmanaged and ownership inspection MUST report no new owned entry
- **AND** deselection and owned-entry cleanup MUST NOT adopt, change or delete it using reconstruction authority

#### Scenario: Existing private proof cannot be bypassed

- **WHEN** existing private evidence for a selection is corrupt, contradictory, drifted or incomplete
- **THEN** independent reconstruction MUST NOT promote that selection to a proven match by ignoring the evidence
- **AND** existing refusal, legacy preservation and target-isolation guarantees MUST remain in force

#### Scenario: Read-only reconstruction leaves absent state absent

- **WHEN** `sync --check` inspects complete, incomplete or invalid independent satisfaction and no ownership-state root existed before the command
- **THEN** that root MUST remain absent and native, legacy and support bytes, permissions and modification times MUST remain unchanged

#### Scenario: Marked legacy twins remain byte-identical

- **WHEN** complete reconstructed native entries coexist with a surviving marked legacy twin
- **THEN** reader-only compatibility behavior MUST preserve the marked legacy twin's bytes, permissions and modification time
- **AND** it MUST NOT infer deletion authority from native equivalence

### Requirement: Missing verified source input establishes no match

The reader SHALL use an exact verified acquisition snapshot or currently authorized directory source, not a guessed identity, mutable substitute or unsigned receipt. Unavailable source input in offline mode, invalid provenance or a source-definition/support snapshot inconsistent with the selected resource SHALL NOT establish independent satisfaction. Original source and mirror fingerprint domains SHALL remain disjoint, and no new source or ownership-state version SHALL be introduced.

#### Scenario: Stale unavailable or unverified source establishes no satisfaction

- **WHEN** required source input is unavailable offline, unverified, stale relative to the selected resource or carries invalid provenance
- **THEN** the client MUST NOT invent a source identity or suppress legacy output using native payload similarity
- **AND** it MUST preserve native, legacy, support and state on read-only refusal

### Requirement: Reader preparation does not switch legacy production writes

Adding source-derived native satisfaction SHALL NOT activate native writes or repository support conversion in the compatibility-reader prerequisite. A selected hook without complete authorized satisfaction SHALL retain the existing legacy emission behavior. Non-hook resources and machine scope SHALL preserve their current behavior, and existing committed acquisition locks SHALL remain byte-compatible.

#### Scenario: Unproven hooks retain ordinary legacy emission

- **WHEN** a selected hook has no complete durable or independently reconstructed native satisfaction
- **THEN** the compatibility-reader client MUST retain its ordinary legacy output behavior without activating a native writer
- **AND** non-hook materialization, machine scope and frozen acquisition-lock compatibility MUST remain unchanged
