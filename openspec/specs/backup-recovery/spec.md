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
