# backup-recovery Specification

## Purpose
Define filesystem authority for backup discovery and recovery so untrusted metadata cannot authorize restore or retention decisions outside admitted backup generations.

## Requirements

### Requirement: Codex backup metadata claims require captured regular manifests

Agent Marketplace Codex backup-set restore, listing and rotation discovery MUST enumerate and read manifests below captured backup-directory authority. Opened manifests MUST remain regular, singly linked and nonblocking at captured size with state/named validation; aliases MUST refuse before metadata claims authorize restore or rotation. Archive-presence metadata queries MUST stay beneath captured directory authority. Genuine optional missing directory and malformed-JSON filtering MAY retain their current semantics; unsafe/read/generation failures MUST NOT supply valid claims. Valid regular manifests MUST still restore the actual archived managed content. Payload/archive mutation consumers remain separate; no arbitrary new processing budget or transaction is introduced.

#### Scenario: Codex manifest alias refuses before restore

- **WHEN** a real complete two-partition backup set is selected through an external manifest alias while current installed content differs from the archive
- **THEN** restore MUST refuse with typed unsafe identity and preserve current/foreign bytes, while the original regular manifest MUST restore the actual archived generation

### Requirement: Restore archive payloads require captured regular authority

Agent Marketplace ordinary restore extraction and Codex archive preflight MUST admit archive payloads through captured regular, singly linked, nonblocking opened-file authority before gzip/tar decoding. Final payload aliases and unsafe shapes MUST refuse without granting archive payload authority. Streaming MUST remain bounded at captured compressed-file size and validate opened/named state; valid regular archives MUST still restore actual archived native content. Cleanup errors MUST propagate. These controls do not add arbitrary expanded/compressed budgets, a transactional extraction, output publication authority or exclusion of uncooperative writers.

#### Scenario: Restore payload hardlink refuses before extraction

- **WHEN** a real created archive selected for restore is multiply linked while current installed content differs from the archive
- **THEN** restore MUST refuse with typed unsafe-file identity before extraction and preserve current/foreign bytes, while a valid regular archive MUST actually restore the archived generation

### Requirement: Regular restore publication retains captured output authority

Agent Marketplace ordinary regular-file and explicitly mapped native-file restore MUST retain captured authorized output parent, strict regular/single-link destination and completed staged identity through publication. Archive bytes MUST stage privately below captured descriptors rather than truncate live destinations. Opened/named target, parent, staged state and admitted absence MUST be revalidated before descriptor-relative rename. Refusal MUST preserve foreign/current/replacement bytes and remove only attempt-owned staged inodes. Valid ordinary archive-mode and mapped permission-mode semantics MUST remain supported without adding per-file fsync, arbitrary new budgets, all-output extraction transaction or exclusion of uncooperative writers. Directory/symlink/source-archive/evidence mutations remain separate.

#### Scenario: Restore file hardlink refuses without foreign write

- **WHEN** a real restore selects a multiply linked regular destination while archive and current user bytes differ
- **THEN** it MUST refuse with typed unsafe-file identity without changing current/foreign bytes, while the regular destination MUST still restore actual archived content and mode

### Requirement: Restore tree namespace mutation stays under captured roots

Agent Marketplace directory and symbolic-leaf extraction MUST verify the named root generation against its captured authority before mutation. Directory preparation and symbolic-leaf parent traversal MUST stay below strict captured descriptors; symbolic namespace removal/link creation MUST NOT reopen an absolute output pathname. Existing entry path and symbolic-target policies MUST remain supported. This namespace control does not claim generation-atomic recursive member deletion, atomic symbolic replacement, all-output transactional extraction or exclusion of uncooperative writers, and adds no processing budget or durability sync.

#### Scenario: Restore tree retarget refuses before foreign mutation

- **WHEN** the extraction root is retargeted while compressed input is consumed after root capture for a directory or symbolic entry
- **THEN** extraction MUST refuse with typed generation identity before creating foreign entries, while valid input MUST really restore the selected directory or symbolic leaf

### Requirement: Backup regular source payloads retain classified captured authority

Agent Marketplace ordinary and explicitly mapped regular backup payload reads MUST remain below their captured source authority. Opened files MUST be regular, singly linked and nonblocking, refuse final aliases and match the classified tar metadata identity/state before streaming. Payload copies MUST stay bounded at captured size with opened/named state validation; unsafe sources MUST NOT yield a successful backup or mutate foreign bytes. Existing symbolic archive entries, format/modes and mapped-only operation with an absent ordinary root MUST remain supported. Directory/symbolic discovery metadata and archive publication/cleanup remain separate, with no arbitrary new budgets, transactional archive promise or writer exclusion.

#### Scenario: Backup source hardlink refuses before payload import

- **WHEN** actual backup creation selects an ordinary or mapped regular source that is multiply linked
- **THEN** it MUST refuse with typed unsafe-file identity and preserve foreign bytes without partial archive residue, while a regular source MUST really archive bytes that restore after the live source changes

### Requirement: Backup discovery and symbolic metadata retain captured source authority

Agent Marketplace ordinary and Codex source selection/tree discovery MUST read metadata beneath captured source descriptors with strict root-relative parent traversal. Symbolic link text MUST be read descriptor-relatively and remain matched to the observed link state without following target payloads. Unsafe ancestors MUST refuse before foreign metadata authorizes a successful archive. Genuine optional absence MAY retain current semantics but MUST be normalized before descriptor cleanup, and disappearing admitted traversal members MUST refuse as generation conflicts. Valid contained final symbolic entries and exact target text, root-dot omission and mapped-only absent ordinary-root operation MUST remain supported. No arbitrary new work budgets or archive transaction is introduced.

#### Scenario: Backup metadata ancestor alias refuses before import

- **WHEN** public backup creation selects symbolic metadata below an external directory alias
- **THEN** it MUST refuse with typed unsafe-path identity and preserve foreign link text/no archive residue, while a contained real parent MUST archive symbolic metadata that restores its exact target text

### Requirement: Archive generation retains captured output and cleanup authority

Agent Marketplace ordinary and Codex staged archive generation MUST create exclusive private0600 outputs beneath captured existing parent descriptors. Completed writer regular/single-link state MUST match the named archive and admitted parent object before success is reported. Refusal cleanup MUST remove only the attempt-created inode from the captured parent, preserve foreign replacements and propagate meaningful close/cleanup errors. Existing initial root/parent alias admission, exclusivity/collision, tar/gzip format and close-only durability semantics MUST remain supported. Namespace preparation, Codex final/manifest publication and retention remain separate; no new budgets, per-file fsync, whole archive transaction or exclusion of uncooperative writers is introduced.

#### Scenario: Archive output refusal keeps foreign replacement

- **WHEN** a shared archive producer retargets the admitted output parent after creation and then fails
- **THEN** refusal MUST retain the original source error, preserve foreign replacement archive bytes and remove only the attempt inode from the captured parent, while ordinary valid Create/Restore and existing output collision controls MUST still work

### Requirement: Codex backup-set publication retains attempt authority

Agent Marketplace Codex backup-set creation MUST retain captured set-directory authority through operation-name reservation, partial archive/manifest creation, final publication and refusal cleanup. Existing final or partial manifest names, including aliases, MUST remain occupied rather than being truncated. New members MUST be exclusively created privately; producer-observed regular/single-link identity/state, final absence and parent object MUST be validated before descriptor-relative publication. Cleanup MUST remove only tracked attempt-created inodes and preserve foreign replacements. Manifest-last completeness, exact root keys, format and archive0600/manifest0644 modes MUST remain supported. No new budget, durability sync, whole-set transaction or uncooperative-writer exclusion is introduced; namespace preparation and retention remain separate.

#### Scenario: Codex set publication preserves aliased partial manifest

- **WHEN** real public Codex backup-set creation encounters a pre-existing partial manifest alias
- **THEN** foreign bytes and original alias text MUST remain unchanged, the creator MUST use an available operation without attempt partial residue, and the resulting set MUST restore actual archived payload through public restore

### Requirement: Ordinary retention deletes only admitted archive generations

Agent Marketplace ordinary archive retention MUST enumerate target/ordinary archive names beneath captured directories, retain the existing two-axis oldest-first policy and preflight selected removals before mutation. Selected archives MUST be regular, singly linked and nonblocking through opened-file validation; unsafe shapes MUST refuse. Each removal MUST revalidate the observed full archive state and named parent before descriptor-relative unlink. Member descriptors MUST close per inspection rather than accumulate across selected archives. Valid regular retention and genuine absent backup-root behavior MUST remain supported. Codex set retention remains separate; no new budget, directory fsync, batch transaction or uncooperative-writer exclusion is introduced.

#### Scenario: Ordinary retention refuses hardlinked archive

- **WHEN** public retention selects a multiply linked old ordinary archive
- **THEN** it MUST refuse with typed unsafe-file identity and preserve old/newest/foreign bytes, while removal of the extra link MUST permit real deletion of the old archive and preserve the newest archive

### Requirement: Codex retention remains authorized by observed manifest generations

Agent Marketplace Codex backup-set retention MUST keep discovery, manifest claims and selected removals beneath captured target-directory authority. Parsed claims MUST retain the opened manifest generation without changing serialized metadata. Selected manifests and archive members MUST admit strict opened regular/single-link generations before any selected-plan mutation. The authorizing manifest generation MUST be revalidated before each archive deletion; each archive and the final manifest unlink MUST match observed state and named parent beneath captured descriptors. Member descriptors MUST close per inspection. Preserve current complete-set filtering/ordering/two-axis policy and manifest-last removal, with no new budget, fsync, batch transaction or uncooperative-writer exclusion.

#### Scenario: Codex retention refuses hardlinked member before set removal

- **WHEN** public rotation selects a complete old Codex set with a multiply linked archive member
- **THEN** it MUST refuse with typed unsafe-file identity before changing old/newest/foreign bytes, while removing the extra link MUST allow real old archive/manifest removal and retain the newest complete set for public restore

### Requirement: Symbolic restore admits safe structural destination leaves

Agent Marketplace symbolic archive restoration MUST inspect destination shape beneath the captured parent before recursive removal. Existing regular files MUST pass shared opened no-follow/nonblocking regular/single-link validation and match classified full state; special leaves MUST refuse. Genuine absence and existing directory/symbolic replacement semantics/exact archived target text MUST remain supported without following target payloads. Typed refusal MUST preserve destination generation and foreign bytes; meaningful descriptor-close failures MUST propagate. Later original-generation leases/tree transactions remain separate; no new budget, blanket initial-root alias ban or uncooperative-writer exclusion.

#### Scenario: Symbolic restore refuses hardlinked destination

- **WHEN** actual public Create archives an inert symbolic target and public Restore encounters an existing destination hardlinked to inert foreign bytes
- **THEN** Restore MUST refuse typed unsafe-file without changing destination inode/mode/mtime or foreign bytes, and removing the extra link MUST allow genuine publication of the exact archived symbolic target

### Requirement: Codex restore retains selected set generations through extraction

Agent Marketplace Codex set restore MUST retain captured set-parent and source archive generations across selection, preflight and extraction. Handles MUST remain bounded by declared native partition count and close on all returns. Before every partition mutation, original authorizing manifest full state, named parent, all source generations and canonical output-root keys MUST remain valid. Replacement members, even byte-identical, MUST refuse typed generation before they authorize writes. Preflight/extraction MUST reuse captured regular/single-link compressed-size reader authority without public archive-path reopening. Initial aliases, genuine missing-backup behavior and late partial-completion semantics remain; no new budget, whole-set restore transaction or uncooperative-writer exclusion.

#### Scenario: Codex restore refuses replaced preflight generation

- **WHEN** public CreateCodexSet archives real inert native bytes and public RestoreCodexSet encounters an identical-byte/mode archive or manifest replacement after preflight
- **THEN** it MUST refuse before native mutation, preserve current native inode/mode/mtime/bytes and replacement member bytes, and restoring the original member MUST allow genuine two-partition extraction of archived content

### Requirement: Completed restore staged payload authority survives final publication

Regular restore MUST compare the final staged name below captured parent authority with the completed written/chmodded descriptor's full identity/state immediately before relative publication. Missing, aliased or replaced stages MUST generation-refuse without publishing foreign bytes; cleanup MUST preserve unknown replacements and current native output. Ordinary archives, explicitly mapped native files and Codex sets MUST retain genuine byte/mode restoration, including unreadable archived modes without reopening payload after chmod. This control does not independently bind later destination/parent actions or supply transactional extraction or uncooperative-writer exclusion.

#### Scenario: Restore staged payload replacement refuses publication

- **WHEN** public ordinary, explicitly mapped or Codex set restore replaces its actual completed staged file at the final publication boundary with independently authored foreign bytes
- **THEN** restore MUST generation-refuse and preserve current/foreign/retained-stage bytes/inodes/modes/mtimes and output membership, and a healthy retry MUST actually restore archived bytes/mode while preserving the earlier foreign stage

### Requirement: Regular restore retains original output action generations

Ordinary, mapped-native and Codex regular restore MUST revalidate the original destination state or absence, captured ancestry and currently named output parent identity at final publication after later validators. Detached original descriptors MUST NOT authorize a successful restore under a replaced named parent. Unknown destination/parent generations MUST remain untouched with typed generation refusal and only creator-stage cleanup. Supported initial aliases, regular/single-link shape policy, unreadable archived stage modes, existing error causes and genuine byte/mode restoration MUST remain valid. This rule does not establish extraction transactions or atomic uncooperative-writer namespace exclusion.

#### Scenario: Restore final destination replacement preserves original authority

- **WHEN** real public ordinary, mapped-native or two-partition Codex restore replaces an admitted destination, creates a foreign entry at admitted absence or replaces the named parent at final publication
- **THEN** restore MUST generation-refuse, preserve original/foreign bytes/inodes/modes/mtimes, foreign parent metadata and output members while cleaning only creator staging, and restored original file/absence/parent authority MUST actually publish archive bytes/mode without changing retained foreign generations

### Requirement: Symbolic restore retains admitted destination leaf generations

Ordinary and Codex symbolic restore MUST compare current no-follow leaf state or absence with the admitted generation immediately before replacement removal below captured parent authority. Later/unknown leaf generations MUST generation-refuse and remain untouched with foreign subtree members and retained original bytes/metadata preserved. Existing regular/single-link structural admission, directory/link/absence replacement behavior and exact archived target creation MUST remain functional. Codex's earlier existing-symlink preflight refusal MUST remain intact. This rule does not bind late named-parent authority, recursively snapshot descendant generations or establish transactional/atomic uncooperative-writer exclusion.

#### Scenario: Symbolic restore preserves late foreign destination generation

- **WHEN** real public ordinary or Codex restore replaces an admitted symbolic-destination leaf or absence at its replacement boundary with a foreign directory containing an inert member
- **THEN** restore MUST generation-refuse without changing foreign tree/member and original bytes/inodes/modes/mtimes/root membership, and restoring admitted authority MUST genuinely recover exact archived symbolic target while earlier foreign tree stays unchanged

### Requirement: Symbolic restore requires original named parent authority

Ordinary and Codex symbolic restoration MUST verify that the currently named parent still resolves to the captured directory object at the replacement action boundary before leaf removal and link creation. Detached original descriptors MUST NOT authorize mutation/success after named-parent replacement. Typed refusal MUST preserve original and foreign parent/leaf bytes, identities, modes, mtimes and membership. Supported initial parent aliases and genuine exact symbolic target recovery MUST remain functional. No recursive descendant snapshot, atomic uncooperative-writer exclusion or whole-operation transaction is established by this rule.

#### Scenario: Symbolic restore refuses detached named parent generation

- **WHEN** real public ordinary or Codex symbolic restore replaces the named parent at the mutation boundary while the admitted leaf remains valid under its detached original descriptor
- **THEN** restore MUST generation-refuse before original deletion, preserving original/foreign parent and leaf metadata/bytes/members, and restoring original parent authority MUST genuinely recover exact archived symbolic target without changing retained foreign parent

### Requirement: Selected archive directories retain admitted generation authority

Ordinary and Codex backup creation MUST match an opened selected directory with its classified original object before walking members, traverse metadata/payloads below that captured selected descriptor and revalidate the named selected directory generation before success. Unknown/replaced selected generations MUST typed-refuse rather than import foreign members into successful archives. Refusal MUST preserve original/foreign bytes and metadata and clean only creator archive output. Existing cleaned tar paths, dot-prefixed selections, root-dot omission, contained symbolic entries, private archive modes and genuine recovery MUST remain valid. This rule does not promise original top-level selection leases, complete recursive-member snapshots, transactional archives or atomic exclusion of uncooperative writers.

#### Scenario: Backup directory generation refuses foreign member import

- **WHEN** public ordinary or Codex backup substitutes a selected source directory after its metadata admission or descriptor opening with a foreign directory containing inert member bytes
- **THEN** creation MUST generation-refuse without published archive/reservation residue, preserve original/foreign directories and payload bytes/inodes/modes/mtimes, and restoring original selected authority MUST create an archive whose actual recovery restores original bytes without foreign members

### Requirement: Nested archive directory descent retains admitted child authority

Ordinary and Codex backup MUST match each classified child directory to a descriptor opened below its captured admitted parent before reading members, use that child descriptor for descendant metadata/payload reads, and validate original named child object before success. Replaced child or captured-ancestor generations MUST refuse rather than authorize foreign payload import. Refusal MUST preserve original/foreign nested directory/payload bytes/inodes/modes/mtimes and leave no archive/reservation residue. Sorted original tar hierarchy, root-dot omission, symbolic text/modes, initial source aliases and actual valid recovery MUST remain supported. Active directory descriptors remain held by recursion; OS resource exhaustion MUST propagate rather than authorize success. No new arbitrary depth/work limit, complete-tree content freeze, archive transaction or atomic exclusion is established.

#### Scenario: Nested backup directory generation refuses foreign descent

- **WHEN** real public ordinary or Codex creation substitutes a classified nested child directory at descent or after opening with a foreign directory/member while the selected root remains original
- **THEN** creation MUST generation-refuse preserving original/foreign bytes/metadata with no archive/reservation residue, and restoring original child authority MUST genuinely create an archive whose recovery restores original bytes without foreign payloads

### Requirement: Backup listing claims retain admitted target directory generations

ListBackups MUST enumerate beneath captured backup-root authority, match each target directory descriptor to its admitted no-follow object identity and revalidate named target generation before returning ordinary archive names or complete Codex set claims. Replacements MUST generation-refuse with no partial listing claims and no mutation of original/foreign alias/directory/member bytes/inodes/modes/mtimes/membership. Genuine absent backup roots, existing ordinary empty targets, initial backup-root aliases, Codex malformed/incomplete filtering and exact listing order MUST remain supported. Per-target descriptors MUST close within their iteration with causal errors propagated. This rule adds no archive validity/content policy, work budget, later restore/rotation action lease, complete directory snapshot or atomic uncooperative-writer exclusion.

#### Scenario: Backup listing refuses replaced target directory generation

- **WHEN** actual public ordinary or Codex backup listing substitutes an enumerated target with a foreign alias or directory containing archive names or genuine complete foreign Codex sets
- **THEN** listing MUST generation-refuse with nil claims preserving all original/foreign bytes/metadata/members, and restoring original target MUST produce exact healthy listing whose actual restore recovers original archived bytes after live modification

### Requirement: Ordinary restore retains selected archive generation authority

Ordinary and mapped Restore MUST retain captured target-directory enumeration and selected archive Destination generation through extraction, verify named selected parent before payload streaming and use existing strict singly linked/nonblocking bounded opened/named payload controls. Post-selection directory aliases/replacements and archive-leaf replacements MUST generation-refuse before native bytes change rather than recapture foreign archive authority. Refusal MUST preserve current native bytes/inodes/modes/mtimes, original/foreign archive and parent metadata/bytes, and leave no output staging residue. Supported initial backup-root/target aliases, missing-backup behavior, archive/mapped modes and real recovery MUST remain valid. Descriptor close failures MUST preserve causes and not report a successful timestamp; selected restore failures MUST retain boundary logging. No new processing budget, archive format/content policy, atomic uncooperative-writer exclusion or all-output transaction is established.

#### Scenario: Ordinary restore retains selected archive and directory generations

- **WHEN** actual ordinary or mapped Create/Restore substitutes an already selected archive leaf, real parent directory or parent alias with an independently created valid foreign tar/gzip generation
- **THEN** Restore MUST generation-refuse preserving current native and original/foreign archive/parent bytes/metadata without output staging residue, and restoring original authority MUST genuinely recover archived original bytes/modes while initial selected aliases remain supported

### Requirement: Backup retention retains admitted target directory generations

Rotate MUST retain actual no-follow target directory object identity beneath captured backup-root enumeration and match the opened target descriptor before ordinary archive or complete Codex set discovery/import. Post-admission target replacement or alias MUST generation-refuse before removal, preserving original/foreign archive/manifest bytes/inodes/modes/mtimes, directory metadata/membership and current native bytes/metadata. Existing member generation, strict bounded reader, deletion/overlap and retention count/day/order/default policies MUST remain. Per-target descriptor lifetimes MUST close within their iteration, retain close/inspection causes and revalidate named parent before successful return. Supported initial backup-root aliases and absent-root no-op MUST remain; no new work budget, content policy, atomic uncooperative-writer exclusion or whole-retention transaction is established.

#### Scenario: Retention refuses replaced target directory generations

- **WHEN** actual ordinary or Codex Rotate replaces an admitted original backup target with a foreign real directory or alias containing two independently public-created valid archives/complete sets
- **THEN** Rotate MUST generation-refuse preserving original/foreign/native bytes/metadata/membership, and restoring original target MUST really retain exactly newest original restorable backup while genuine initial backup-root aliases keep working

### Requirement: Ordinary backup namespace preparation retains captured parent authority

Ordinary and mapped Create MUST prepare missing backup directories relative to the nearest admitted existing directory descriptor, verify that selected parent's named generation before mutation, and retain prepared parent authority through existing private archive publication. Late selected parent aliases or real-directory replacements MUST generation-refuse without creating directories/archives or changing original/foreign/native bytes/inodes/modes/mtimes/membership/aliases. Cold and warm creation, supported initial external HOME/data/backups/target aliases, private archive modes, missing mapped-only source roots and empty-source no-op MUST remain. Existing target-alias listing skip policy MUST not change. Descriptor Close failures MUST propagate causes. This adds no arbitrary budget, content/durability policy, atomic uncooperative-writer exclusion or all-directory rollback.

#### Scenario: Ordinary backup namespace refuses retargeted parent

- **WHEN** public ordinary or mapped-only Create retargets an admitted closest existing HOME/data/backups/target parent to a foreign alias or real directory before namespace preparation
- **THEN** creation MUST generation-refuse preserving all original/foreign/native state without directory/archive residue, and restoring original authority MUST create a real private archive whose Restore recovers original bytes while initial aliases and empty-source no-op remain valid

### Requirement: Codex backup namespace retains captured parent generation

CreateCodexSet MUST prepare missing set-directory segments relative to the nearest admitted existing directory descriptor, verify that selected named parent's generation before mutation and retain the prepared authority through existing partition/archive/manifest publication and owned cleanup. Late selected HOME/data/backups/target aliases or real-directory replacements MUST generation-refuse with empty operation and no directory/archive/manifest residue or mutation of original/foreign/native bytes/inodes/modes/mtimes/membership/aliases. Cold/warm creation, supported initial external aliases, private archive and manifest modes and valid empty two-partition set behavior MUST remain. Descriptor Close failures MUST retain causes. No new global budget, content/durability policy, atomic uncooperative-writer exclusion or all-directory rollback is established.

#### Scenario: Codex backup namespace refuses retargeted parent

- **WHEN** public CreateCodexSet with actual content in both native partitions retargets its admitted closest existing HOME/data/backups/target parent to a foreign alias or real directory before preparation
- **THEN** creation MUST generation-refuse preserving all original/foreign/native state without namespace/archive/manifest residue, and restoring original authority MUST publish a genuine complete set whose real RestoreCodexSet recovers both partitions while initial aliases and empty sets remain supported

### Requirement: Codex restore root preparation retains captured namespace authority

RestoreCodexSet MUST retain captured existing partition root or nearest admitted existing parent through missing-root preparation and extraction, verify named generations before mutation and prepare missing segments by descriptor. Extraction MUST reuse prepared root authority instead of recapturing a substituted pathname. Late root/parent aliases or real-directory replacements MUST generation-refuse before changing original/foreign native trees, namespace membership/metadata or later partitions and preserve archive/manifest evidence without stage residue. Genuine absent/nested roots, supported initial parent aliases, unsafe-root refusal, native modes and actual both-partition recovery MUST remain. Borrowed root descriptors MUST stay caller-owned and Close errors preserve causes; ordinary extraction retains its existing owned-root lane. No new budget, content/durability policy, full output transaction/rollback or atomic uncooperative-writer exclusion is established.

#### Scenario: Codex restore namespace retains original root authority

- **WHEN** actual public two-partition Codex restore substitutes a verified existing root or absent/nested-root parent with a foreign alias or real directory before preparation/extraction
- **THEN** restore MUST generation-refuse preserving original/foreign/later-partition bytes/inodes/modes/mtimes/membership and evidence without namespace/stage residue, and restoring original authority MUST genuinely prepare roots and recover both archived partitions while supported initial parent aliases and unsafe-root refusal remain
