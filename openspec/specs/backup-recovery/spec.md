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
