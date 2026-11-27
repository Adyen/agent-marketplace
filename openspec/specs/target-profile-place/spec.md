# target-profile-place Specification

## Purpose

Place delivery lets users select Marketplace packages for uncompiled application profiles, approve the exact host context, and safely deploy, verify, and retire owned installations.

## Requirements

### Requirement: Profiles are selected and locked by exact identity

A place target profile SHALL be selected and locked by its exact identity and full profile digest. Before any acquisition or write, the following MUST refuse with a diagnostic naming the profile:

- ambiguity between built-in and external profiles;
- an unsupported target ID;
- a malformed path;
- an invalid scope.

#### Scenario: Ambiguous or unsupported profiles refuse before writes

- **WHEN** a selection names a profile ID provided by both a built-in and an external source, or names an unsupported target ID
- **THEN** the command MUST refuse before acquisition and leave configuration, acquisition locks and target roots unchanged

### Requirement: Assessments bind the exact profile

A package assessment SHALL bind the package identity, its mapping and the exact full profile digest, both for online delivery and for offline delivery from cache. A stale or mismatched assessment MUST refuse before acquisition or writes.

#### Scenario: Assessment binds the exact profile online and offline

- **WHEN** a package was assessed against one profile digest and the selected profile's digest differs
- **THEN** online and offline `sync` MUST refuse that package before acquisition or writes, naming the package and the profile

### Requirement: Target enable and disable change only the target ID

`target enable <id>` SHALL add only the target ID, and only for a selected, known profile that organization policy admits. `target disable <id>` SHALL remove only the target ID. Both commands MUST keep the profile selection and MUST NOT retire an installation. When the user root has no authored target list, they MUST write `harnesses:`.

#### Scenario: Enable requires a selected, admitted profile

- **WHEN** `target enable <id>` names a profile that is not selected, is unknown, or is blocked by organization policy
- **THEN** the command MUST refuse and leave configuration unchanged

#### Scenario: Disable preserves the profile selection and installation

- **WHEN** `target disable <id>` runs for an enabled target with an installed package
- **THEN** only the target ID MUST be removed from the list, the profile selection MUST remain, and the installed files and their ownership records MUST remain

### Requirement: Approval receipts bind the exact host context

An interactive approval receipt SHALL record:

- the exact OS, target ID and profile digest;
- the canonical target root and the canonical state store;
- the declared command roles;
- when commands exist, the canonical absolute executable and its digest.

When the profile declares no commands, the receipt MUST record that no execution is authorized. Executable bindings MUST be rechecked immediately before execution.

#### Scenario: A changed executable invalidates its approval

- **WHEN** the executable digest no longer matches its approval
- **THEN** execution MUST refuse naming the mismatch and run no host command

### Requirement: Organization preapproval is exact

Organization preapproval SHALL match an exact canonical target root and an exact canonical state root under admitted platform bases. Wildcards, prefixes and raw environment values MUST NOT grant approval.

#### Scenario: A mismatched state root produces no receipt

- **WHEN** organization preapproval names a different canonical state root
- **THEN** no receipt MUST be produced or used, and placement MUST refuse naming the mismatch

### Requirement: Approval never relocates or deletes

Approval SHALL NOT authorize deleting files under any root other than the one it binds. When the resolved target root, a root alias, the target's home environment value or the XDG state root changes, placement MUST require a new approval. That approval describes a separate installation, and the old root MUST remain untouched.

#### Scenario: A changed root needs new approval and never relocates

- **WHEN** the resolved target root, a root alias, the target's home environment value or the XDG state root changes after approval
- **THEN** placement MUST refuse until a new approval is given, and the new approval MUST leave the old root's files untouched

### Requirement: Retirement is explicit and bound to a recorded installation

Retiring an installation SHALL require a stable installation ID, the recorded root and state, and interactive confirmation. Non-TTY use and `--yes` MUST refuse. Retirement MUST remove only unchanged owned files and MUST keep edited and unowned bytes.

#### Scenario: Retirement keeps edited and unowned files

- **WHEN** a user retires an installation in which one owned file was edited and an unowned file sits beside the package files
- **THEN** unchanged owned files MUST be removed, the edited file and the unowned file MUST remain byte-for-byte, and the edited file MUST be reported as ceded

#### Scenario: Non-interactive retirement refuses

- **WHEN** retirement runs without a TTY or with `--yes`
- **THEN** it MUST refuse and change no file or record

### Requirement: Direct selection of an unusable target refuses

A direct selection of a disabled, unavailable, unapproved or policy-blocked target SHALL refuse before acquisition, config or lock mutation. Malformed or repository-scope-ineligible authored selections MUST remain errors.

#### Scenario: Direct selection of a disabled target refuses without writes

- **WHEN** `install` directly selects a package whose target is disabled, unavailable, unapproved or policy-blocked
- **THEN** the command MUST refuse before acquisition and leave configuration and acquisition locks unchanged

#### Scenario: Malformed and repository-scope selections remain errors

- **WHEN** an authored selection is malformed, or a repository root selects a place target that is eligible only for the user scope
- **THEN** the command MUST refuse with an error rather than skip the selection

### Requirement: Collections and headless sync skip unusable targets

A collection MAY keep known members for disabled or unavailable targets and SHALL report each skipped member with its reason. Headless `sync` MUST skip targets still waiting for consent, report a target diagnostic and continue independent work. `sync --check` MUST stay read-only and keep the existing strict-diagnostic exit policy. No change in consent, availability or environment MUST prune files.

#### Scenario: Collections keep and report skipped members

- **WHEN** a selected collection contains a member for an unavailable target
- **THEN** sync MUST keep the member selected, MUST NOT place it, and MUST report it as skipped with its reason

#### Scenario: Headless sync skips pending consent and continues

- **WHEN** non-interactive `sync` runs with one target pending approval and other selected resources
- **THEN** it MUST skip the pending target with a target diagnostic, complete the other work, and prune nothing for the pending target

### Requirement: Place planning refuses unowned collisions

Before any target publication, place delivery SHALL plan the complete desired package set. Any collision with an unowned existing file MUST refuse that package, even when the bytes match. Place delivery MUST NOT adopt any file.

#### Scenario: Identical unowned file refuses the package

- **WHEN** a package would place a file at a path that holds an unowned file with identical bytes
- **THEN** the package MUST refuse, the file MUST remain unowned and unchanged, and no other file of that package MUST be published

### Requirement: Place conflicts are deterministic whole-package refusals

Both colliders in a new/new conflict SHALL refuse, whatever the iteration order. An installed package that conflicts MUST keep its complete installed projection, and its new revision MUST refuse as a unit. Collision keys MUST compare path components as a trie, normalizing each component by NFD, Unicode case-folding, then NFC on all hosts.

#### Scenario: New/new conflicts refuse independent of order

- **WHEN** two newly selected packages claim the same normalized path, with either selection order, including a case-fold collision, an NFC/NFD twin, or a file against another package's directory prefix
- **THEN** both packages MUST refuse deterministically

#### Scenario: An installed package keeps its projection on conflict

- **WHEN** a new revision of an installed package conflicts with another owner
- **THEN** the installed files MUST remain unchanged, and the new revision MUST refuse as a unit

### Requirement: Paths being pruned stay reserved

A path that the plan removes SHALL stay reserved for its installed package until its prune completes, even after the ownership commit excludes it. A new package claiming such a path MUST refuse for that run.

#### Scenario: A deselected owner blocks a new claimant until pruned

- **WHEN** one package is deselected and a newly selected package claims one of its paths in the same run
- **THEN** the new package MUST refuse for that run

### Requirement: Oversized place journals refuse before staging

A package set whose fully marked worst-case journal would exceed the dependency journal size limit SHALL refuse before staging, so that no pending journal is unreadable.

#### Scenario: Oversized journal refuses before staging

- **WHEN** a package set's worst-case journal would exceed the dependency journal size limit
- **THEN** place delivery MUST refuse before creating any stage or target file

### Requirement: Place packages build as retained immutable generations

Place delivery SHALL build each package as an immutable generation in agent-marketplace state, outside every target root and the acquisition cache. Garbage collection MUST NOT remove a generation that a committed place projection or a pending place journal references.

#### Scenario: Generations stay referenced while needed

- **WHEN** garbage collection runs while a committed place projection or a pending place journal references a generation
- **THEN** that generation MUST NOT be removed

### Requirement: Place staging is journaled and outside the target root

Before creating the stage directory, place delivery SHALL write a durable dependency-journal intent recording the planned stage path (without a stage identity), payload manifest, generation key, ordered writes, journaled directory creations and root identity. Place delivery MUST create the place-stage directory under agent-marketplace state, outside the target root, and durably record the new directory's identity in the journal before writing any payload to the stage.

#### Scenario: Staging happens outside the target root

- **WHEN** a package is placed
- **THEN** no staging or temporary file MUST appear in the target root, and the stage MUST be under agent-marketplace state

### Requirement: Cross-filesystem place publication refuses

Before any target write, place delivery SHALL compare the staging filesystem with the effective target parent, or with its nearest existing ancestor inside the captured root when the parent does not exist, and refuse when they differ. An `EXDEV` rename MUST NOT fall back to copying.

#### Scenario: Cross-filesystem targets refuse before writes

- **WHEN** the staging filesystem differs from the target parent's, or from its nearest existing ancestor inside the root
- **THEN** placement MUST refuse before any target write and report the device mismatch, not `dependency.recovery-required`

#### Scenario: EXDEV after earlier writes stays pending without fallback

- **WHEN** a rename returns `EXDEV` after an earlier target write
- **THEN** the journal and stage MUST remain, no copy fallback MUST run, and later mutating recovery MUST refuse the same rename until the filesystem layout is resolved

### Requirement: Place directories are created and removed by recorded identity

Place delivery SHALL create missing directories only from the journaled list, marking each one before creating it and recording its identity afterwards. It MUST NOT create an application root, and a missing approved root MUST refuse before staging. A directory MUST be removed only when it is empty and `SameObject` with its recorded identity.

#### Scenario: Created directories are removed only when empty and identical

- **WHEN** a later transaction deselects a package whose directory was created by a recorded place transaction
- **THEN** the directory MUST be removed only if it is empty and `SameObject` with its recorded identity, and a non-empty directory, for example one holding a ceded edited file, MUST be retained with its record

#### Scenario: A missing approved root refuses

- **WHEN** the approved target root does not exist
- **THEN** placement MUST refuse before staging and create no directory

### Requirement: Place writes follow dependency order then commit and prune

Place delivery SHALL publish package files in dependency order, with mods before layouts. After all files are published it MUST commit ownership, and then prune stale files. Each removal is marked in the journal before it happens. Packages are pruned in reverse dependency order, with each package's files first and then its directories deepest-first.

#### Scenario: Dependent packages publish in order

- **WHEN** a layout and its required mod are placed together, and later both are deselected
- **THEN** the mod MUST be published before the layout, and the layout MUST be removed before the mod

### Requirement: Place roots are bound to their pinned identity

Place delivery SHALL pin the approved canonical root's directory identity when it admits the plan. Immediately before every publish or removal it MUST re-stat the named root and require `SameObject`; otherwise it MUST refuse with a generation conflict. Destination leaves and honored links MUST be revalidated before each publish or removal.

#### Scenario: A replaced root refuses publish and prune

- **WHEN** the approved root is renamed and a directory is recreated at its path between planning and writing
- **THEN** publication and pruning MUST refuse, and no file MUST be written or removed through the new directory

### Requirement: Only the recorded canonical root spelling carries ownership

Place mutation, ownership claims in status, and retirement SHALL refuse any root spelling that is not the recorded canonical root, including a case variant on a case-insensitive host, and an alias MUST NOT open a new empty ownership store. When the recorded XDG state root is unavailable, ownership MUST be reported as unavailable and mutation MUST refuse.

#### Scenario: A root alias or replaced destination symlink is not ownership proof

- **WHEN** a command addresses the root through an alias or a case-variant spelling, or a destination symlink is replaced between plan and write
- **THEN** mutation and ownership claims MUST refuse, and no redirected write or prune MUST happen

#### Scenario: Changed XDG state root reports ownership unavailable

- **WHEN** `XDG_STATE_HOME` changes and the recorded state root is unavailable
- **THEN** status MUST report ownership as unavailable, place mutation and retirement MUST refuse, and existing files MUST NOT be treated as a first installation

### Requirement: Pending place journals block other work

While a place journal is pending, read-only commands such as `status` and `sync --check` SHALL return `dependency.recovery-required` without replay, and any command other than the admitted recovery MUST refuse. A mutating sync MUST recover only the admitted transaction from its journal and recorded generations, before validating or planning new work.

#### Scenario: Read-only commands refuse a pending place journal without replay

- **WHEN** a crash leaves a place journal pending at any interruption point
- **THEN** `status` and `sync --check` MUST return `dependency.recovery-required` without changing any byte

### Requirement: Recovery before the first target write clears only exact stages

When no target write has happened and the planned stage path is absent, recovery SHALL clear the intent. If a stage is present without a matching recorded identity, recovery MUST stop and retain both stage and journal. Even when the identity matches, recovery MUST remove the stage only if every present file is a byte prefix of its manifest entry; otherwise it MUST stop and retain both stage and journal. Recovery MUST NOT resume publication.

#### Scenario: Incomplete staging is cleared, never resumed

- **WHEN** a crash happens after the first stage file, or after staging completes but before the first target write
- **THEN** mutating recovery MUST clear an exact transaction-owned stage and its intent without publishing, and if one stage byte is replaced it MUST stop with the stage and journal intact

### Requirement: Recovery completes only the exact recorded prefix

After some target writes, recovery SHALL complete the recorded prefix of directory creations and publications only while each destination is exactly its old or intended generation. It MUST recopy absent stage files from the retained generation, checking each digest and mode before publishing. A marked directory that is present without a recorded identity, or that is not `SameObject`, MUST stop recovery and be retained.

#### Scenario: A partially published prefix completes exactly

- **WHEN** a crash happens after a published prefix, after a journaled directory creation, or after a stage file was deleted following the first publication
- **THEN** mutating recovery MUST complete exactly the recorded suffix, recopying absent stage files from the generation, and if one destination generation was changed it MUST stop without deleting any later file or evidence

#### Scenario: An unrecorded present directory stops recovery

- **WHEN** a crash happens after a directory creation was marked but before its identity was recorded, and a directory is present at that path
- **THEN** recovery MUST stop and retain the directory with the journal

### Requirement: Recovery commits ownership once

When all package files are published but ownership is not committed, recovery SHALL verify every published file against the journal and then commit the matching ownership generation exactly once. An interruption during the ownership-state rename MUST resolve to the state before or after it.

#### Scenario: Ownership commits once after full publication

- **WHEN** a crash happens after all package files were published but before the ownership commit, or during the ownership-state rename
- **THEN** recovery MUST commit the matching ownership generation exactly once, or recognize the committed new generation, without reacquiring packages

### Requirement: Prune recovery removes only recorded stale generations

During prune recovery, a stale file SHALL be removed only when its bytes and parent match the recorded old generation. A marked absent entry counts as removed; an unmarked absent stale file MUST stop recovery. An edited stale file MUST be left in place and reported as a retained user file.

#### Scenario: Prune recovery treats marked and unmarked absences differently

- **WHEN** a crash happens during pruning after one removal was marked, and another stale file is absent without a mark
- **THEN** the marked absent entry MUST count as removed, and the unmarked absent file MUST stop recovery with the journal intact

#### Scenario: Edited stale file survives prune recovery

- **WHEN** a stale file is edited and a crash happens before its prune
- **THEN** recovery MUST leave the edit in place and report it as a retained user file

### Requirement: Recovery stops name their evidence and never guess

Every recovery stop SHALL name the journal path and the stage, directory or destination path where it stopped, both in `status` and in the mutating error. Recovery MUST NOT infer ownership from matching content, MUST NOT delete a stage, target or journal whose generation differs from its record, and MUST NOT run host code.

#### Scenario: A recovery stop names its paths

- **WHEN** recovery stops on a changed destination
- **THEN** `status` and the mutating error MUST name the journal path and the destination path, and no file MUST be removed

### Requirement: Edited and deleted package files keep the projection

When any member of an installed package is edited or deleted outside a crash, sync SHALL keep that package's entire current projection, MUST NOT recreate or partly refresh any file, and MUST report the change as drift. An upgrade that finds such a change MUST keep the complete old projection and refuse the new revision as a unit.

#### Scenario: Edited file keeps the whole package and blocks upgrade

- **WHEN** one installed package file is edited and a newer package revision is selected
- **THEN** sync MUST keep every installed file of that package, report the edit as drift, and refuse the new revision as a unit

#### Scenario: Deleted file is treated as an edit

- **WHEN** a user deletes one installed package file and runs sync with no pending journal
- **THEN** sync MUST NOT recreate the file, MUST keep the other files, and MUST report the deletion as drift

### Requirement: Deselection retires owned files and cedes changed ones

On deselection, an unchanged file SHALL be removed only when its bytes and parent match the recorded generation. Edited files MUST be left byte-for-byte and ceded from ownership, and deleted paths MUST be ceded. Matching bytes at an unowned path, or a prior package name, MUST NOT cause adoption.

#### Scenario: Deselection retires owned files and cedes edits

- **WHEN** a package with one edited file and one deleted file is deselected
- **THEN** unchanged owned files MUST be removed, the edited file MUST remain byte-for-byte, both paths MUST be reported as ceded, and neither MUST be re-adopted later

### Requirement: Initial-load verification results are recorded per generation

After successful publication, ownership commit and pruning finish for all approved Place work admitted to a sync invocation, sync SHALL run the approved initial-load checks. If publication, ownership commit or pruning fails for any admitted Place root, no initial-load check from that invocation SHALL run. A selected target skipped as unusable is not admitted and MUST NOT block the barrier. After the journal clears, sync MUST record each completed check result in one ownership-state update: pass or fail against the exact profile, package generation, check identity and host version. A committed generation with no recorded result is verification-pending.

#### Scenario: Crash recovery leaves verification pending, distinct from edits

- **WHEN** recovery completes a pending publication, and separately a user later edits an installed file
- **THEN** recovery MUST leave verification pending, and status MUST report the edit as drift, without labeling either event as the other

#### Scenario: A failed Place root defers checks for admitted work

- **GIVEN** one sync invocation admits Place work for roots A and B
- **WHEN** publication for B fails while A's publication could otherwise complete
- **THEN** no approved initial-load check from that invocation MUST run, and A MUST remain verification-pending

#### Scenario: Completed check outcomes preserve pass and fail evidence

- **GIVEN** all admitted Place roots publish, commit ownership and prune successfully
- **WHEN** one approved initial-load check fails and another passes
- **THEN** the checks MUST run only after the admitted work is complete, and status MUST retain the failed and passed results against their exact generations

#### Scenario: A skipped unusable target does not block the admitted-work barrier

- **GIVEN** one admitted Place root has an eligible failed or pending check and another selected target is skipped as unusable
- **WHEN** sync runs
- **THEN** the admitted root's check MUST run and the skipped target MUST NOT block it

### Requirement: Failed verification keeps files and retries

A failed check SHALL keep the placed projection and its generation reference and MUST NOT roll files back. The next mutating sync MUST retry a failed or pending check against unchanged files, and a later user edit MUST be reported as drift, not as a failed check. Recovery and read-only commands MUST run no host code, and read-only commands MUST report recorded results.

#### Scenario: Failed verification keeps files and retries

- **WHEN** the approved check fails after placement
- **THEN** the package files and ownership MUST remain, `status` MUST report the failure without running code, and the next mutating sync MUST retry the check

### Requirement: Target verify runs only the named approved check

`target verify <locator> --installation <installation-id>` SHALL run only the named approved check and record exact revisions, host-version status, the result and its evidence source, without installing, activating or repairing anything. When package files have drifted, it MUST report the drift and refuse without running a check or writing anything.

#### Scenario: Target verify refuses drifted files without running checks

- **WHEN** `target verify` runs for an installation whose files include an edit
- **THEN** it MUST report the edited path as drift and refuse, run no host command, and write no result, config, lock, approval, ownership or target file

#### Scenario: Target verify runs only the named check

- **WHEN** `target verify <locator> --installation <id>` runs for an unchanged installation
- **THEN** it MUST run only that approved check, record its exact revisions, host-version status, result and evidence source, and install, activate or repair nothing

### Requirement: Marked file mutations must be durable before recovery advances

During mutating Place recovery, a marked regular-file or ownership publication whose only matching generation is intended and whose stage is absent SHALL sync the verified effective destination parent before completion. A marked regular-file prune with an absent leaf SHALL sync its verified parent before completion. Sync failure MUST leave the operation pending without advancing progress; retry MUST finish without repeating the mutation. Genuine no-ops and read-only inspection are exempt.

#### Scenario: Marked file mutation retries parent sync

- **WHEN** a marked file publication, ownership publication, or regular-file prune has the visible result described above and the verified parent sync fails during recovery
- **THEN** recovery MUST return the sync error with the same pending operation and completed count, and a later successful sync MUST allow recovery to finish without repeating the mutation

### Requirement: Place package acquisition requires indexed prerequisites

Online and cache-only Place package acquisition MUST require a known prerequisite array before reading or publishing package archive bytes. An explicit empty array is known; absent or `null` prerequisites are unknown and MUST refuse without writing cache data.

#### Scenario: Online package acquisition requires indexed prerequisites

- **WHEN** an otherwise eligible exact Place revision has absent or `null` prerequisites
- **THEN** acquisition MUST refuse before requesting its archive or writing package cache data

#### Scenario: Cache-only unknown prerequisites refuse before archive reads

- **WHEN** a locked Place package has missing or `null` cached prerequisite metadata
- **THEN** cache-only acquisition MUST refuse with `marketplace.cache.place-prerequisites-missing` before reading its archive
- **AND** it MUST instruct the user to re-fetch online

#### Scenario: Explicit indexed-empty prerequisites allow offline acquisition

- **WHEN** a locked Place package has cached `requires: []` and its other lock-bound evidence is valid
- **THEN** cache-only acquisition MUST accept the known-empty declaration and verify the cached archive
