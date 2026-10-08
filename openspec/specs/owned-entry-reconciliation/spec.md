# owned-entry-reconciliation Specification

## Purpose
Defines how Agent Marketplace reconciles entries in shared harness aggregate
files against durable ownership state, preserving handwritten entries and
failing closed.

## Requirements

### Requirement: Unproven entries are preserved

An existing entry SHALL be treated as unmanaged unless durable ownership
state proves it is an owned entry; unmanaged entries SHALL be left unchanged.

#### Scenario: Handwritten entries remain unmanaged

- **WHEN** an aggregate entry has no ownership-state record
- **THEN** sync SHALL preserve it unchanged

#### Scenario: Missing ownership state preserves everything

- **WHEN** the ownership state is missing or unreadable
- **THEN** every existing entry SHALL be preserved and none SHALL be pruned

### Requirement: Unmanaged collisions refuse only the generated entry

When a desired generated entry shares the identity of an unmanaged existing
entry, sync SHALL preserve the existing entry and refuse only the generated
entry; valid sibling entries SHALL still materialize.

#### Scenario: Unmanaged collision fails only the generated entry

- **WHEN** a desired MCP key already exists in the file as an unmanaged entry
- **THEN** the existing entry SHALL be preserved and only the generated entry
  SHALL fail

### Requirement: Proven owned entries reconcile against recorded state

A proven owned entry SHALL be kept when its value matches the record,
restored when it has drifted, and pruned only when no longer desired; an
unmanaged entry SHALL never be pruned.

#### Scenario: Drifted owned entry is restored

- **WHEN** a proven owned entry's current value differs from the record
- **THEN** sync SHALL replace it with the configured value

#### Scenario: Stale owned entries are pruned

- **WHEN** a proven owned entry is present but no longer desired
- **THEN** sync SHALL remove it and no unproven entry

### Requirement: Repair candidates partition by value equality

An unmanaged existing entry whose identity is desired SHALL be offered for
adoption when its value equals the desired value, and as a replacement
candidate when it differs; the offers SHALL be disjoint.

#### Scenario: Identical values are offered for adoption

- **WHEN** an unmanaged desired entry's value equals the generated value
- **THEN** repair SHALL offer byte-neutral adoption

#### Scenario: Differing values are offered for replacement

- **WHEN** an unmanaged desired entry's value differs from the generated value
- **THEN** repair SHALL offer it for replacement and never for adoption

### Requirement: Gemini shared scopes reconcile together

When the repository root and user home identify the same directory, including filesystem aliases, Agent Marketplace SHALL reconcile the shared Gemini native settings file once against the combined desired entries. It MUST preserve handwritten content and existing permissions, and identical synchronization MUST NOT rewrite the file. An entry SHALL be pruned only when it is no longer desired in either scope. Conflicting values for one MCP identity and operations that cannot isolate the requested scope MUST refuse before materialization.

#### Scenario: Gemini shared settings retain both scopes

- **WHEN** repository and machine selections share one Gemini settings file
- **THEN** MCP servers and hooks from both scopes MUST remain present alongside handwritten entries
- **AND** repeated synchronization and check-only inspection MUST report no drift without rewriting bytes or modification time
- **AND** removing one scope's selections MUST preserve the other scope and handwritten entries

#### Scenario: Gemini shared identity conflict refuses mutation

- **WHEN** two scopes desire different values for the same MCP identity in one Gemini settings file
- **THEN** materialization MUST report `materialize.destination.overlap`
- **AND** existing settings and ownership state MUST remain unchanged

#### Scenario: Gemini shared scope-restricted mutation refuses

- **WHEN** a scope-restricted operation targets a Gemini root shared by repository and machine scopes
- **THEN** it MUST report `materialize.destination.overlap` before writing or pruning
- **AND** the existing settings MUST remain unchanged

### Requirement: Shared Gemini native work does not introduce ordinary pruning

When shared Gemini native settings reconciliation adds machine-scope work without independently compiled or blocked machine resources, Agent Marketplace SHALL reconcile the shared native settings without running an additional ordinary artifact or skill pruning pass. Repository commands MUST remain selected through synchronization and check-only inspection in both materialization modes, including filesystem aliases of HOME. An empty desired set SHALL retain ordinary stale-output cleanup.

#### Scenario: Shared Gemini native work preserves repository commands

- **WHEN** repository commands are selected at a materialization root that identifies HOME, with or without repository native settings fragments
- **THEN** first and repeated synchronization MUST preserve the commands in both materialization modes
- **AND** check-only inspection MUST report no drift and leave their bytes and modification times unchanged
- **AND** stale repository commands MUST still be pruned when an active repository selection no longer desires them

#### Scenario: Empty shared Gemini desired set retains cleanup

- **WHEN** the last desired repository command and native settings entries are removed from a shared Gemini root in symlink materialization mode
- **THEN** synchronization MUST remove the stale command and owned native settings entries
- **AND** it MUST preserve handwritten settings

### Requirement: Repair uses verified acquisition authority

`doctor --repair` SHALL compute Git-backed desired entries from valid acquisition locks and verified exact snapshots. Missing or invalid Git acquisition evidence SHALL cause nonzero exit without reconciling that evidence or changing shared aggregate entries.

#### Scenario: Missing or invalid Git acquisition evidence refuses repair

- **WHEN** repair is requested for a Git-backed resource whose acquisition lock is missing or invalid
- **THEN** repair SHALL exit nonzero and leave the acquisition lock and shared aggregate unchanged

#### Scenario: Moving a Git ref does not change repair values

- **WHEN** a registry ref moves after its acquisition lock was created and a conflicting entry is confirmed for replacement
- **THEN** repair SHALL install the exact snapshot's value and leave the acquisition lock byte-identical

### Requirement: Repair refuses incomplete organization selection

When organization resource acquisition fails, repair SHALL report that failure and refuse mutation rather than interpret unavailable resources as no longer desired.

#### Scenario: Organization acquisition failure refuses repair

- **WHEN** the organization Marketplace cannot resolve its configured resources and no organization acquisition lock is deployed
- **THEN** repair SHALL report the acquisition failure, exit nonzero, and leave shared aggregate entries unchanged

### Requirement: Repair publication uses unchanged policy

Repair SHALL validate the configuration and acquisition-lock inputs authorizing its preview after confirmation and at the aggregate publication boundary. A changed input SHALL cause a generation conflict, preserve the aggregate and ownership state, discard uncommitted staging, and stop repair before prompting another target.

#### Scenario: Configuration changes during confirmation

- **WHEN** configuration changes while repair waits for confirmation
- **THEN** repair SHALL refuse the action with nonzero exit and preserve the aggregate

#### Scenario: Organization acquisition lock appears during confirmation

- **WHEN** an organization acquisition lock is deployed while repair waits for confirmation
- **THEN** repair SHALL refuse the action and preserve both the aggregate and the deployed lock

#### Scenario: Policy changes between confirmation validation and mutation

- **WHEN** policy changes after confirmation was validated but before its repair mutation begins
- **THEN** repair SHALL report a generation conflict without changing the aggregate

#### Scenario: Policy changes after repair staging

- **WHEN** policy changes after adoption, replacement, or orphan removal is staged but before publication
- **THEN** repair SHALL refuse publication, preserve aggregate and ownership bytes, and remove that attempt's staged file, journal, and backup

#### Scenario: Repair stops at the first policy conflict

- **WHEN** policy changes before a confirmed action in a multi-target repair
- **THEN** repair SHALL refuse that action and SHALL NOT prompt for subsequent targets

### Requirement: Repair confirmation does not hold mutation locks

Repair SHALL hold no shared-aggregate ownership lock while waiting for a user's confirmation.

#### Scenario: Ownership writers remain unblocked at confirmation prompts

- **WHEN** repair waits for confirmation before or after a completed action
- **THEN** another writer SHALL be able to acquire the aggregate's ownership lock

### Requirement: Durable aggregate reads retain canonical-root opened-file authority

Agent Marketplace durable aggregate reconciliation, repair, safety-backup, compare-and-swap and recovery live-payload readers MUST consume through captured authority below the ownership store's recorded canonical root. Relative path validation and strict ancestors MUST refuse intermediate/final aliases and traversal before payload reads. Opened files MUST remain regular, singly linked and nonblocking at captured size with state/named-object validation. Genuine absence MUST retain errors.Is and legacy os.IsNotExist; unsafe/generation or cleanup failures MUST NOT become absence. Explicit target-root aliases canonicalized at store admission MUST remain supported. Refusal MUST preserve original/foreign bytes and metadata. Publication/state/journal/archive mutation remains separate; no arbitrary new limits, transaction or exclusion of uncooperative writers is introduced.

#### Scenario: Scoped aggregate reader refuses intermediate alias

- **WHEN** a canonical-root aggregate reader encounters an intermediate alias to an external regular payload after a valid native read control
- **THEN** it MUST refuse with typed unsafe-file identity before returning bytes and preserve foreign payload bytes, identity, mode and mtime

### Requirement: Durable aggregate publication retains captured authority through staging

Agent Marketplace ownership Apply MUST capture authorized aggregate parent and destination generation before journal preparation, then exclusively stage and publish descriptor-relatively under that authority. Retargeted parents MUST NOT receive private staged bytes. Captured destination and completed staged generation MUST be revalidated before durable rename; same-byte replacement generations MUST refuse with ownership generation-conflict identity/code. Shared existing modes and private creation MUST remain supported. Refusal cleanup MUST remove only attempt-owned staged inodes through original captured parents; foreign staged replacements and injected-crash recovery evidence MUST remain intact. Existing journal/crash ordering and durability contract MUST remain supported. Recovery/state/archive mutation remains separate; no arbitrary new limits or exclusion of uncooperative writers is introduced.

#### Scenario: Durable staging refusal preserves authority

- **WHEN** ownership Apply encounters a parent retarget after journal preparation or a same-byte destination/staged replacement after staging
- **THEN** it MUST refuse without foreign private staging or replacement overwrite, preserve native/user bytes and clean only attempt-owned refusal staging while valid shared-mode/crash controls continue working

### Requirement: Durable ownership metadata requires captured read authority

Agent Marketplace ownership state and journal readers MUST stay beneath the canonical selected state-root authority recorded at Store.Open and strict captured ownership/key descriptors. Opened metadata MUST remain regular, singly linked, nonblocking and bounded at captured size with opened/named state validation; unsupported aliases/unsafe shapes MUST refuse before proof or recovery decisions. Genuine optional absence MUST normalize before deferred cleanup; cleanup/unsafe failures MUST NOT become missing state/no journal. Preserve state-corrupt coded diagnostics and filesystem cause identity, zero unmanaged proof on refusal, JSON formats, missing/uninitialized distinction, valid recovery, public selected StateRoot spelling and initial explicitly selected state-root aliases. Publication and recovery cleanup remain separate, with no new byte/work limit, transaction or durability promise.

#### Scenario: Aliased ownership state cannot grant proof

- **WHEN** public Proof encounters valid matching state through an external leaf alias
- **THEN** it MUST return coded corrupt and typed filesystem refusal with zero unmanaged proof while preserving foreign/aggregate bytes; restoring real state MUST grant actual owned proof

#### Scenario: Aliased journal cannot authorize recovery

- **WHEN** public recovery encounters a real prepared-crash journal through an external leaf alias
- **THEN** it MUST refuse with typed filesystem identity and no outcomes while preserving alias/foreign/aggregate bytes; restoring the real journal MUST perform actual rollback

### Requirement: State and phase-journal publication stays under captured record authority

Agent Marketplace state and phase-journal publication MUST capture strict existing canonical state/ownership/key parent authority before private staging, retain exclusive600 staged producer identity, validate produced opened/named generations and parent before durable descriptor-relative publication, and clean only tracked attempt-created staged inodes beneath captured directories. Commit MUST NOT recreate a missing record parent. Public prepared-directory/JSON/crash semantics and private600 metadata MUST remain unchanged. This namespace publisher does not retain a prior loaded metadata lease across an entire mutation; prior-generation binding, namespace preparation, safety metadata and journal/recovery cleanup remain separate. No new budget, whole-mutation transaction or uncooperative-writer exclusion is introduced.

#### Scenario: Retargeted record parent cannot receive metadata publication

- **WHEN** public Apply reaches after-aggregate-rename and its record parent is retargeted to foreign state/journal files
- **THEN** metadata publication MUST refuse typed unsafe/conflict before foreign metadata/staging writes, preserving both foreign payloads and the admitted prepared journal while the already-published aggregate remains next; valid Apply MUST still commit readable private metadata without staging residue

### Requirement: Journal cleanup removes only the observed journal generation

Agent Marketplace Apply, CAS refusal and recovery journal cleanup MUST retain private captured parent and actual opened or published-producer journal identities without changing serialized JSON. Cleanup MUST revalidate the observed parent object and full file state before descriptor-relative removal. Replaced or disappeared observed journals MUST refuse rather than deleting replacements or reporting successful absence. Shared metadata read/write paths MUST preserve strict canonical parent verification and genuine optional read absence. Failure-injection semantics remain supported after authority validation. Record publication leases, staged/safety cleanup and namespace evidence remain separate; no whole-mutation transaction or uncooperative-writer exclusion is introduced.

#### Scenario: Journal cleanup preserves replacement generation

- **WHEN** actual Apply replaces its journal with foreign inert bytes after actual state commit
- **THEN** cleanup MUST refuse typed generation identity and preserve replacement/original metadata while actual native/state remain committed; restoring the original journal MUST let public recovery report already-committed and really remove it

### Requirement: Recovery staged cleanup requires aggregate-scoped recorded payload authority

Agent Marketplace CAS and recovery staged cleanup MUST reject arbitrary metadata-nominated paths: staged paths MUST be absolute, share aggregate parent and match the aggregate's generation-specific staged prefix beneath canonical root authority. Captured opened staged files MUST remain regular, singly linked and nonblocking; bytes read at captured size MUST match the recorded desired digest before removal. Revalidate named parent and use descriptor-relative generation-checked removal. Preserve empty collision-disarmed paths and genuine validated stage absence, current journal format and valid crash recovery. Restart cleanup uses recorded digest plus opened generation, not persisted inode ownership; identical-byte replacements after process death are not distinguished. Safety cleanup/prior record leases remain separate; no new budget, transaction or uncooperative-writer exclusion is added.

#### Scenario: Recovery staged cleanup refuses foreign path authority

- **WHEN** public Recover reads a real prepared-crash journal modified to nominate an inert outside-root staged file
- **THEN** it MUST refuse typed unsafe-path/no outcomes and preserve foreign/live/stage/evidence while restoring the true staged path MUST allow actual rollback and stage/journal removal

### Requirement: Safety manifest evidence requires captured read authority

Agent Marketplace retained safety-manifest evidence MUST enumerate beneath captured canonical state/safety/root-key/file-slug directory authority and read sidecars through shared opened regular/single-link/nonblocking metadata admission at captured size with state/named-object checks. Unsupported aliases/unsafe shapes/generation changes and metadata cleanup failures MUST refuse rather than nominate orphan removal or become ordinary unusable provenance. Revalidate named safety directory before returning evidence. Preserve sorted records, public paths/JSON and genuine absent/malformed/ordinarily unreadable sidecar filtering with current ownership state/journal behavior. Payload admission/RawBytes/publication/retention/evidence-action leases remain separate, with no new budget, durability, transaction or uncooperative-writer exclusion.

#### Scenario: Aliased safety manifest cannot nominate orphan removal

- **WHEN** actual orphan nomination for an unmanaged live entry reads matching safety sidecar claims through an external alias
- **THEN** it MUST return typed filesystem refusal and no candidates while preserving foreign metadata/native bytes, and restoring regular sidecar MUST let actual valid evidence nominate the retained entry

### Requirement: Retained safety payload reads retain admitted generation authority

Agent Marketplace SafetyBackups MUST admit payload shape and opened regular/single-link/nonblocking state beneath captured safety namespace without reading payload bytes during listing. Returned backups MUST privately retain original scope/name, parent-object and full payload identity; RawBytes MUST use that admitted authority rather than reopen mutable public path claims, compare observed states, read at captured size and revalidate named parent before returning bytes. Unsupported aliases/changed generations/links/special files and cleanup failures MUST return no payload. Unadmitted fabricated records MUST NOT grant read authority. Preserve faithful prior bytes/public path/manifest/JSON and allow harmless directory timestamp changes while checking device/inode. Publication/retention/evidence-action leases/namespaces remain separate; no new budget, durability, transaction or uncooperative-writer exclusion.

#### Scenario: Retained safety payload alias refuses before reading

- **WHEN** an actual listed retained backup payload is moved and replaced with an external alias
- **THEN** RawBytes MUST return typed filesystem refusal/no bytes and preserve original/foreign payloads, while restoring and relisting the regular backup MUST return exact actual prior bytes

### Requirement: Safety publication uses captured private namespace and producer authority

Agent Marketplace Apply/BackupOnly MUST prepare ownership/safety namespaces through strict captured directory components with private creation and preserve existing directory modes. Safety payload creation/sync and sidecar staging/publication MUST use one captured safety parent, revalidate named parent and actual produced payload state before publishing claims, and remove only tracked attempt inodes on refusal. Preserve byte-exact prior payloads, private 0700 directories/0600 files, current filenames/JSON/crash behavior and no foreign writes or replacement deletion on refusal. Retention/CAS safety cleanup and prior record/action leases remain separate; no new budget, overall transaction or uncooperative-writer exclusion is introduced.

#### Scenario: Safety publication refuses retargeted directory

- **WHEN** actual BackupOnly retargets its prepared safety directory to an external directory before payload creation
- **THEN** it MUST return typed filesystem refusal/no path with unchanged native/foreign bytes and no attempt residue, while restoring the directory MUST permit real readable byte-exact private safety publication

### Requirement: Safety retention removes only selected captured generations

Agent Marketplace safety retention MUST enumerate beneath captured canonical safety namespace, preserve name-based five-newest policy, and capture every selected payload/sidecar regular-single-link opened generation before removing any member. Removal MUST revalidate named parent and full observed file state or genuine observed absence before descriptor-relative deletion. Unsafe shapes/replacements/cleanup failures MUST refuse and preserve affected members. Do not retain one descriptor per selected member. Payload/sidecar pair removal is not an atomic transaction; CAS cleanup and other leases remain separate, without new budgets or uncooperative-writer exclusion.

#### Scenario: Safety retention preserves linked selected members

- **WHEN** public BackupOnly retention selects an old safety payload linked to an external counterpart
- **THEN** retention MUST refuse typed unsafe-file before changing old payload/sidecar/foreign bytes, while removing that link MUST permit actual five-newest retention with readable backups

### Requirement: CAS safety cleanup retains actual producer authority

Agent Marketplace CAS refusal cleanup MUST retain private actual safety writer parent/payload/published-sidecar generations in the in-memory journal without JSON changes, validate both members before mutation and remove only those observed generations descriptor-relatively. Replacement/unsafe members MUST preserve both replacement and paired evidence while returning the original CAS refusal through the existing cleanup warning boundary. Empty-directory cleanup MUST compare original object and use kernel empty-only removal, with nonempty a harmless no-op. Healthy cleanup MUST still remove its actual attempt. Restart recovery retains safety evidence and MUST NOT fabricate this private authority from BackupPath. No new budget, atomic cleanup transaction or uncooperative-writer exclusion.

#### Scenario: CAS cleanup preserves replaced safety backup

- **WHEN** actual Apply's produced safety payload is replaced after staging and late policy refuses publication
- **THEN** cleanup MUST preserve replacement/original/paired evidence and live prior bytes while stage/journal cleanup occurs, and a healthy late refusal MUST really remove its own backup/sidecar/empty directory/stage/journal

### Requirement: Apply retains originally admitted ownership-record generation

Agent Marketplace Apply MUST privately retain the originally opened state file/parent generation and genuine absence alongside loading without changing public Record/JSON or existing absence diagnostics. It MUST revalidate that admission after final policy validation before native rename and at captured state publication. Same-JSON replacement MUST NOT authorize stale native or metadata publication. Allow creation of an initially absent parent while requiring admitted state absence; existing parents remain object-bound. Late state refusal preserves replacement and durable recovery journal but may follow native publication. Restart recovery/action leases remain separate, with no persisted inode guarantee, new budget, whole transaction or uncooperative-writer exclusion.

#### Scenario: Original record replacement refuses publication

- **WHEN** actual Apply's originally loaded generation1 record is replaced after staging with identical JSON
- **THEN** it MUST refuse typed generation identity before changing native/replacement/original bytes, while restoring original MUST let a real generation2 native update succeed

### Requirement: Ownership locks admit safe opened metadata leaves

Agent Marketplace ownership lock acquisition MUST open final leaves without following aliases or blocking on special files, and validate opened regular/single-link shape before kernel acquisition, after a wait succeeds and before diagnostic truncation. Initial unsafe shape MUST refuse with typed filesystem cause and no held session. Late unsafe metadata shape MUST preserve bytes at the existing best-effort diagnostic boundary while kernel lock/release semantics remain intact. Preserve timeout, cancellation, reentrancy, unsupported-kernel fallback and supported selected state/shared locks-directory alias exclusion. Directory namespace pinning remains separate; no new budget, transaction or uncooperative-writer exclusion.

#### Scenario: Lock admission refuses hardlinked metadata

- **WHEN** public LockContext sees a lock leaf hardlinked to inert foreign bytes
- **THEN** acquisition MUST refuse before metadata writes and preserve foreign bytes/session absence, while a real regular leaf MUST acquire, write the current pid diagnostic and release normally

### Requirement: Ownership lock mutation retains selected namespace authority

Agent Marketplace MUST record the canonical selected lock namespace at Store.Open without changing read-only failure timing, preserve initial selected state/shared-directory aliases, and prepare/open persistent lock leaves under strict captured directory authority. Revalidate selected root/named opened identity before admission, after contention and before metadata writes. Retargeted directories/replaced leaves MUST NOT authorize foreign writes or stale sessions. New directories MUST be private; existing modes and kernel timeout/cancel/reentrant/fallback/exclusion semantics remain unchanged. A concurrent creation refusal MAY use one noncreating descriptor-relative open of the existing leaf but MUST NOT delete/recreate lock inodes. Late diagnostic failure stays best effort. No new budget, overall transaction or uncooperative-writer exclusion.

#### Scenario: Lock directory retarget preserves foreign metadata

- **WHEN** a public Store's admitted lock directory is retargeted to an inert foreign directory before Lock
- **THEN** it MUST refuse without changing foreign diagnostic/original namespace and restoring the original MUST allow real kernel acquisition/diagnostic publication/release

### Requirement: Recovery propagates state filesystem admission refusal

Agent Marketplace recovery MUST distinguish actual state metadata read/admission/generation/cleanup refusal from ordinary malformed JSON or genuine missing state. Filesystem refusal MUST preserve original cause identity/public diagnostic and stop before phase decisions or staged/journal cleanup. Genuine optional absence MUST normalize before deferred cleanup; existing missing/malformed-state recovery behavior remains. Original action-generation leases remain separate, with no new budget, recovery transaction or uncooperative-writer exclusion.

#### Scenario: Recovery state refusal preserves prepared generation

- **WHEN** actual Apply commits generation1, prepares generation2 then crashes after staging and its state is replaced by a foreign alias
- **THEN** Recover MUST refuse without changing native/foreign/original state or prepared stage/journal, and restoring original state MUST let genuine rollback remove stage/journal

### Requirement: Orphan deletion remains bound to admitted evidence generation

Agent Marketplace orphan removal MUST retain the fresh candidate's original opened authorizing metadata generation within its write session, preserving parent object and full file state without changing serialized manifests/journals. After existing policy validation and before native publication, the original safety-manifest or journal metadata MUST remain valid under captured strict state-directory authority. Aliased, absent or replaced metadata MUST refuse with original filesystem cause/generation identity. Refusal MUST preserve native/foreign/replacement evidence and clean only attempt-owned stage/journal/backup members. Restoring genuine metadata MUST permit confirmed backed-up deletion. Existing re-nomination/consent/policy semantics remain; no new budgets, whole-action transaction or uncooperative-writer exclusion.

#### Scenario: Orphan removal refuses retargeted authorizing evidence

- **WHEN** actual Devin or Codex sync creates retained provenance, missing ownership state yields a real orphan preview, and authorizing metadata is aliased or replaced with identical JSON after action staging
- **THEN** public confirmed RemoveOrphan MUST refuse before native deletion, preserve native/foreign/replacement evidence and clean attempt stage/journal, while restoring original metadata MUST permit real deletion with a fresh backup

### Requirement: Restart recovery actions retain original metadata authority

Agent Marketplace recovery MUST retain the originally opened journal and ownership-record identity/absence through phase decisions. Before recovery actions and within roll-forward state publication, original full metadata file state and captured parent object MUST remain valid. Identical JSON at a replacement inode MUST NOT authorize stale state overwrite or staged cleanup. Refusal MUST preserve replacement/original metadata and native/staged bytes; original restoration MUST permit real recovery. Existing missing/ordinary malformed-state behavior and JSON stay compatible. The existing optional Crasher MAY expose the read-to-action boundary; production installs none. No new budget, whole recovery transaction or uncooperative-writer exclusion is introduced.

#### Scenario: Recovery refuses original journal replacement before action

- **WHEN** actual public Apply crashes with a prepared or aggregate-renamed journal and public Recover observes its metadata, then original journal or state is replaced with identical JSON
- **THEN** Recover MUST refuse before stale state publication or staged cleanup, preserve original/replacement/native bytes and allow genuine roll-forward/rollback after original metadata is restored

### Requirement: Journal updates retain their originally published metadata generation

Agent Marketplace journal phase and staged-collision disarm publication MUST remain bound to the previously published journal observation. Original full journal state and captured parent object MUST validate before namespace preparation and again in staged metadata publication. An identical JSON replacement inode MUST NOT authorize overwrite/state commit/success; typed refusal MUST preserve original/replacement metadata identity/mode/mtime/bytes and remove only attempt staging. Initial unobserved journal creation, format/private modes and ordinary metadata writes remain compatible. Native already published before a late phase refusal remains published with original prepared recovery evidence; no whole native/metadata transaction, new processing budget or uncooperative-writer exclusion.

#### Scenario: Journal phase update preserves original generation

- **WHEN** public Apply publishes native next bytes and the existing after-aggregate-rename boundary replaces its original prepared journal with identical JSON/private mode
- **THEN** phase publication MUST refuse typed generation before overwriting replacement or committing state, preserve original/replacement/no metadata staging, and restoring original prepared evidence MUST allow real public recovery roll-forward/commit/cleanup

### Requirement: Ownership metadata publication retains produced staging authority

Ownership state, journal and safety-manifest publication MUST retain produced staged identity through final parent/original-generation and staged-reader validation before rename. A replacement stage MUST generation-refuse without overwriting original destination metadata/absence or deleting the replacement. Refusal MUST preserve original/foreign bytes/inodes/modes/mtimes, valid durable recovery evidence and established native publication phases. Healthy recovery and retries MUST genuinely commit matching state/journal generations and raw-byte safety manifests with existing private modes. Post-rename pin mismatch MUST NOT substitute for pre-action validation. No new processing budget, content/durability policy, whole transaction rollback, complete snapshot or atomic uncooperative-writer exclusion is established.

#### Scenario: Metadata publication retains produced staged generation

- **WHEN** actual Apply or BackupOnly replaces the produced metadata stage before state, prepared-journal, journal-phase or safety-manifest publication
- **THEN** publication MUST generation-refuse preserving original destination/absence, both staging generations and recovery evidence, and genuine recovery/retry MUST commit the intended ownership generation or produce the real native safety bytes and matching manifest without owned residue

### Requirement: Prepared recovery cleanup retains original metadata after native observation

Prepared ownership recovery MUST revalidate original state/journal generations after bounded live aggregate observation and before decisions consume staging/journal evidence. Late same-JSON state replacements or aliases MUST typed-refuse with no successful outcomes and preserve original/foreign bytes/inodes/modes/mtimes/links, valid journal/stage evidence and native prior/handwritten bytes or absence. Restored original authority MUST still perform genuine present/absent rollback or abandonment, retain committed generation and preserve the established foreign-change reporting contract. Ordinary malformed state recovery compatibility MUST remain. No new processing budget, content/durability policy, full transaction rollback, complete snapshot or atomic uncooperative-writer exclusion is established.

#### Scenario: Recovery cleanup rechecks original state after aggregate read

- **WHEN** real prepared recovery replaces original state with a same-JSON regular file or external alias after live aggregate observation before rollback/abandon decisions
- **THEN** recovery MUST refuse preserving original/replacement metadata, stage/journal evidence and native bytes/absence, and restoring original authority MUST genuinely roll back or abandon only the owned prepared generation with the established outcome and diagnostic
