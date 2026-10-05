# materialization Specification

## Purpose
Defines how selected resources appear in harness targets, including stable
commands in shared hook files across repository and machine install scopes.

## Requirements

### Requirement: Machine-scope hook commands are location-independent

Agent Marketplace SHALL expand `${DROID_PLUGIN_ROOT}` in a machine-scope
Factory hook command to the absolute machine harness target root. Its
materialized command bytes MUST NOT depend on whether sync runs from the
user's home or a repository. Repository-scope Factory hook commands SHALL use
the relative repository harness target root.

#### Scenario: Machine-scope hook command uses the absolute machine root

- **WHEN** a user-selected Factory hook command contains `${DROID_PLUGIN_ROOT}` and sync runs from the user's home or from a repository
- **THEN** the machine-scope hook file MUST contain the same command with the absolute machine root in both cases
- **AND** `sync --check` MUST succeed from either location after synchronization

#### Scenario: Repository-scope hook command stays repository-relative

- **WHEN** a repository-selected Factory hook command contains `${DROID_PLUGIN_ROOT}`
- **THEN** the repository-scope hook file MUST contain a command rooted at the relative `.factory` path

### Requirement: Harness target roots remain within their authorized scope

Agent Marketplace SHALL admit every selected harness target root before materialization writes below that root. Repository roots MUST remain within the repository scope. Explicit user-owned machine directory aliases MAY retain their existing relocation behavior. An explicitly configured absolute CODEX_HOME MAY relocate the Codex configuration partition without relocating its skills partition. Repository and HOME scope anchors MAY be filesystem aliases. A contained repository directory-root alias MAY be honored, but an escaping repository root, dangling or cyclic root alias, or non-directory root MUST be refused without changing the root object or any external target bytes, permissions or modification time. Refusal MUST carry `materialize.destination.error`. Independent safe non-Codex targets MAY continue; Codex destination preflight MUST refuse before destination mutation when any active partition root is unsafe.

#### Scenario: Unsafe harness root preserves external state

- **WHEN** a selected repository harness target root is an escaping link or a selected harness root is a dangling link, cyclic link or non-directory object
- **THEN** materialization MUST report `materialize.destination.error` and preserve the root object and external state

#### Scenario: Contained root and scope aliases remain usable

- **WHEN** a repository or HOME is a filesystem alias and a harness root is a real directory or a contained directory alias
- **THEN** materialization MUST install selected resources at their native paths in both supported materialization modes without replacing the aliases

### Requirement: Output-directory ancestors below a harness root are real directories

Agent Marketplace MUST reject unsafe symlinked or non-directory output-directory ancestors below an admitted harness target root before writing that target's planned output. The established Factory `skills -> ../.agents/skills` legacy migration MAY replace that exact link without following or changing its target. Missing output directories MAY be created under the authorized root. Refusal MUST report `materialize.destination.error`, preserve the unsafe ancestor and external state, and MUST NOT leave attempt-owned staging residue. Read-only checking MUST validate these ancestors without creating output directories.

#### Scenario: Unsafe output ancestor refuses before target writes

- **WHEN** a selected resource's output-directory ancestor below its harness root is a link or non-directory object
- **THEN** materialization MUST report `materialize.destination.error` before target output changes and preserve the unsafe ancestor and external state

#### Scenario: Read-only root admission creates no output

- **WHEN** read-only materialization checks a safe but missing harness root or output-directory ancestor
- **THEN** it MUST report drift without creating output directories

### Requirement: Diagnostic root summaries remain available on boundary refusal

Doctor and status native destination summaries MUST render a typed filesystem-boundary refusal as an unresolvable root while retaining independent target information. Diagnostic-only doctor MUST NOT read a non-directory root as aggregate payload or change it. Structural configuration failures such as relative CODEX_HOME MUST retain their existing hard-error behavior, and repair MUST still refuse unsafe destination roots before aggregate work.

#### Scenario: Doctor continues diagnosing an unsafe native root

- **WHEN** diagnostic-only doctor encounters a non-directory Codex root
- **THEN** it MUST report an unresolvable root, retain other target information, and preserve the root's bytes and metadata without displaying its payload

### Requirement: Cache-backed link publication retains destination authority

Agent Marketplace MUST bind cache-backed symlink staging, publication and attempt-owned cleanup to the admitted destination directory. A parent or leaf whose observed generation changes before publication MUST be refused without replacing the new object or leaving attempt-owned staging in the captured or replacement tree. Refusal MUST report `materialize.write.error`. Existing user-owned collisions MUST remain preserved, and an unchanged managed link MUST remain a no-op. Existing file-mode output MAY migrate back to a cache-backed link only under its established ownership policy.

#### Scenario: Cache link publication refuses a replaced parent

- **WHEN** a cache-backed link's destination parent is replaced after staging and before publication
- **THEN** publication MUST refuse, preserve replacement content and remove only its own staged entry from the captured directory

#### Scenario: Cache link publication refuses a replaced leaf

- **WHEN** a cache-backed link's destination leaf is replaced after staging and before publication
- **THEN** publication MUST refuse and preserve the replacement leaf and the cache target

#### Scenario: Managed cache links update and repeat without mutation

- **WHEN** a managed cache link requires a new target and later repeats the same desired target
- **THEN** the update MUST publish at the native destination without changing cache payload bytes, and the repeat MUST preserve link identity and modification time

### Requirement: Marker alias payload reads retain scoped authority

Agent Marketplace MUST admit marker-merge JSON aliases before reading payload bytes, for mutating and read-only operations. An effective target outside repository/HOME scope, the admitted machine output directory and the trusted cache root MUST be refused without copying foreign bytes into output or changing the alias or target. Accepted targets MUST be regular and singly linked, and their data and permissions MUST describe one unchanged captured generation. Dangling, cyclic, special or multiply linked targets MUST refuse. Contained aliases and legacy cache migrations MAY detach into regular native output while preserving handwritten content and original target state. Refusal MUST report `materialize.write.error`. This contract does not add an absolute input-size or aggregate-work budget.

#### Scenario: Marker merge refuses an external alias before reading

- **WHEN** a repository marker aggregate aliases a foreign regular JSON file outside admitted scope and cache
- **THEN** materialization MUST refuse before importing its payload and preserve the alias and original target bytes and metadata

#### Scenario: Contained marker aliases preserve native content and no-ops

- **WHEN** an accepted contained marker alias contributes handwritten JSON to a desired native MCP aggregate
- **THEN** the desired and handwritten entries and existing permissions MUST be preserved in a regular output, the original target MUST remain unchanged, and repeat sync MUST preserve output identity and modification time

#### Scenario: Read-only marker merge refuses an external alias

- **WHEN** read-only materialization encounters an external marker aggregate alias
- **THEN** checking MUST refuse without reading its payload or changing the alias

### Requirement: Skill support payload reads stay within captured source authority

Agent Marketplace MUST enumerate skill cache support sources through captured directory handles and authorize each opened source payload against its captured source root before reading. Payloads MUST be regular singly-linked snapshots with unchanged identity and size, supplying bytes and permissions from the same generation. Contained relative file aliases MUST remain usable. Escaping aliases and unsafe payload shapes MUST refuse without importing external bytes. Applicable captured-size growth checks do not introduce a new absolute source or aggregate-work budget.

#### Scenario: Support copy refuses a source link outside its root

- **WHEN** a native skill source support file is an alias outside the authorized source root
- **THEN** cache preparation MUST report its stable write diagnostic before native skill publication and preserve external bytes and identity/mode/mtime

#### Scenario: Support copy keeps contained relative alias content and mode

- **WHEN** a native skill source has a relative file alias to regular executable support data within its captured root
- **THEN** actual native installation and repeat sync MUST retain intended support bytes/mode and leave the original source unchanged

#### Scenario: Support source refusal preserves an installed cache generation

- **WHEN** an installed native skill update changes compiled content but its support source refuses admission
- **THEN** cache preparation MUST preserve installed compiled/support bytes and publish no refused support payload, cleaning only attempt-owned staging

### Requirement: Hook support source refusal preserves native hook content

Agent Marketplace MUST enumerate hook support sources through captured directory handles and authorize regular singly-linked snapshots before reading payload. Byte content and permissions MUST come from the same admitted unchanged generation. Source read/admission failure MUST block affected hooks and support artifacts with registered severity-error diagnostic `materialize.hook.support-refused`, without echoing payload values or installing a partial source support set. Installed affected native content MUST be retained. Existing source/target exclusions, optional absent context, contained relative aliases and conflict arbitration MUST remain compatible. This introduces no new absolute source or aggregate-work threshold.

#### Scenario: Hook support source refusal preserves installed native content

- **WHEN** an installed native hook update has an external source support alias
- **THEN** actual native materialization MUST report source refusal, preserve installed hook/support and external bytes/identity/mode/mtime, and publish no foreign support

#### Scenario: Contained hook support alias keeps native content and mode

- **WHEN** a native hook source support alias points to a regular executable file inside its captured directory
- **THEN** native installation and repeat sync MUST retain hook/support bytes and executable mode, leave the source untouched and avoid rewriting unchanged support

#### Scenario: Hook source refusal keeps independent valid native work

- **WHEN** one selected hook support source refuses while a distinct source has a valid hook and support payload
- **THEN** materialization MUST block only the refused source's hooks/support and still emit the independent valid native hook/support

#### Scenario: Targets without hook support lanes do not read unneeded sources

- **WHEN** Gemini native hooks update settings while source siblings are not part of a support-file output lane
- **THEN** uncopied source siblings MUST NOT block the valid native settings update or become support artifacts; handwritten settings and native permissions remain preserved

### Requirement: Portable manifest reads retain scoped ownership authority

Agent Marketplace MUST authorize portable manifest payloads below a captured repository/HOME scope before decoding ownership claims. The manifest leaf MUST be a direct regular singly-linked file; aliases and special or multiply linked leaves MUST refuse without being read as ownership payload. Existing contained parent and scope aliases MAY remain supported, while escaping parent aliases MUST refuse. Admitted bytes MUST describe one unchanged opened generation with captured-size streaming/growth checks; this adds no absolute byte or aggregate-work threshold. Absence remains valid. Refusal MUST report materialize.manifest.error and MUST NOT authorize a file-mode native output update from refused claims.

#### Scenario: Portable manifest alias refusal preserves native output

- **WHEN** an existing native file-mode skill update encounters a manifest leaf alias to foreign ownership data
- **THEN** materialization MUST report materialize.manifest.error, preserve installed native content, manifest alias and foreign bytes/identity/mode/mtime, and import no foreign ownership claims

#### Scenario: Contained portable manifest parent alias retains native updates

- **WHEN** a repository or machine portable manifest parent aliases a directory within its captured scope
- **THEN** actual native file-mode installation/update and read-only checking MUST remain valid, preserving the parent alias and existing ownership semantics

#### Scenario: Read-only portable manifest parent refusal is not absence

- **WHEN** a portable manifest parent aliases a missing directory and checking runs without mutation
- **THEN** materialization MUST report materialize.manifest.error rather than treating the unsafe parent as absent, preserve the alias and create no output

### Requirement: Portable manifest mutation retains loaded namespace authority

Agent Marketplace MUST bind portable ownership-manifest publication/removal to the admitted scope, parent and loaded file generation. Existing scope/parent objects observed during loading MUST still match before staging or removal. The manifest MUST remain a direct regular singly-linked object with unchanged full identity and bytes, or remain absent when publication loaded absence. Publication MUST stage and rename through captured directories, and removal MUST unlink only the loaded unchanged manifest. Observed parent/leaf drift after staging MUST refuse with materialize.manifest.error, preserve replacement/external bytes, identity, permissions, modification time and aliases, and remove only attempt-owned staging in the captured directory. This is not a whole-installation transaction or exclusion of uncooperative writers between check and mutation, and adds no absolute byte or work threshold.

#### Scenario: Portable manifest removal preserves a replaced generation

- **WHEN** a valid native file-to-symlink migration replaces its loaded manifest generation before an empty-state removal
- **THEN** native migration may complete but manifest removal MUST report materialize.manifest.error and preserve the replacement manifest bytes/identity/mode/mtime

#### Scenario: Portable manifest publication preserves a replaced generation

- **WHEN** valid native migration leaves retained user-owned portable entries while the loaded manifest generation is replaced before publication
- **THEN** publication MUST refuse with materialize.manifest.error while preserving the replacement manifest and retained native user content

#### Scenario: Portable manifest staging keeps captured namespace authority

- **WHEN** a repository or machine manifest parent or leaf is replaced after the changed manifest is staged
- **THEN** metadata publication MUST refuse without mutating replacement/external content or aliases and MUST clean its own staged name from the captured directory

#### Scenario: Missing captured manifest parent stays absent on refusal

- **WHEN** a previously loaded manifest parent disappears before changed ownership metadata is published
- **THEN** publication MUST refuse with materialize.manifest.error without recreating the missing parent or changing the retained original manifest

### Requirement: Portable manifest updates retain faithful permissions and no-ops

Agent Marketplace MUST retain the existing admitted manifest permissions when publishing changed ownership state. Newly created manifests MUST retain 0644 permissions. Identical desired manifest bytes MUST preserve file identity and modification time after generation validation. Missing manifests remain valid, valid contained scope/parent aliases remain usable, and actual native ownership updates and valid empty-manifest removal MUST continue.

#### Scenario: Portable manifest repeats preserve identity and update permissions

- **WHEN** native repository or machine file-mode output repeats unchanged, updates content and later removes its owned selection
- **THEN** the manifest repeat MUST preserve identity/mode/mtime, the real ownership update MUST publish changed bytes with existing permissions, and empty-state removal MUST remove its unchanged manifest

### Requirement: Portable virtual source hashes retain captured payload authority

Agent Marketplace MUST authorize skill support payloads used by read-only portable source hashing against one captured source root. Enumeration and regular singly-linked payload reads MUST be descriptor-relative, preserve one unchanged opened generation and apply captured-size streaming/growth checks without introducing a new absolute size/work budget. Contained relative file aliases and executable permissions MUST remain usable, while foreign/unsafe aliases and payload shapes MUST refuse with materialize.write.error before their bytes contribute to an ownership digest. Read-only checking MUST create no output/cache/manifest state and MUST preserve source/external bytes, identity, permissions and modification time.

#### Scenario: Read-only portable source hash refuses foreign aliases

- **WHEN** read-only file-mode materialization encounters a skill support alias outside its captured source root
- **THEN** checking MUST refuse with materialize.write.error, preserve the source alias and external bytes/metadata, and create no output/cache/manifest state

#### Scenario: Contained portable source hashes retain native ownership

- **WHEN** a file-mode skill installed with a contained relative executable support alias is subsequently checked read-only
- **THEN** the virtual source digest MUST retain the installed native content/mode and ownership meaning, report in sync and preserve the manifest identity/mode/mtime

### Requirement: Portable regular output digests validate opened-file authority

Agent Marketplace portable ownership hashing of regular output leaves MUST read through a captured parent/destination and validate a regular singly-linked nonblocking opened file against its observed and captured full identity. Streaming MUST be bounded by captured size and reject opened-state or target-generation changes through the read. Unsafe regular leaves MUST retain typed refusal before those bytes establish replacement/adoption/pruning authority, preserving installed/external bytes and generations on the refused native update. Valid regular leaves MUST retain the existing digest encoding, permissions and payload semantics without new absolute processing budgets or whole-file buffering. Directory enumeration, structural symlink hashing and mutation generation checks remain separate contracts; no whole-tree snapshot is introduced.

#### Scenario: Portable output digest refusal preserves native generation

- **WHEN** a valid native portable installation's regular payload becomes multiply linked before its ownership digest is consulted for an update
- **THEN** materialization MUST report a coded refusal and retain the filesystem error identity without replacing native/external generations or changing manifest bytes, while valid regular digest encoding remains compatible

### Requirement: Portable tree digest traversal retains captured directory authority

Agent Marketplace portable ownership tree digests MUST enumerate and descend through captured directories, not independently reopened directory pathnames. Observed/captured directory identity and state MUST remain validated through enumeration and descendant reads, and parent-relative entry generations MUST be rechecked before accepting a digest. A replaced or retargeted directory MUST retain typed refusal and MUST NOT import foreign payload bytes. Legacy lexical ordering, relative paths, modes, regular bytes and structural symlink text MUST remain compatible; structural symlinks MUST NOT be followed for payload hashing. No new absolute processing budget, whole mutable-tree atomic snapshot or publication/removal transaction is introduced.

#### Scenario: Portable tree digest refuses replaced directory authority

- **WHEN** a directory classified for portable ownership hashing is replaced or retargeted before enumeration
- **THEN** digesting MUST retain a filesystem refusal without accepting foreign bytes or changing external generations, while valid ordered and structural-link digest controls remain compatible

### Requirement: Portable regular-file publication preserves admitted namespace generations

Agent Marketplace portable regular-file output publication MUST stage and publish below captured authorized parent directories using descriptor-relative operations. The observed destination leaf full identity/state and named parent object MUST be revalidated before replacing a file or migrating a managed link; refusal MUST preserve replacement and foreign bytes, the admitted original and manifest bytes, and remove attempt-owned staged names through their captured parent. Cache source reads MUST retain regular/singly-linked/nonblocking opened-file authority and captured-size validation. Valid native updates, existing no-op ownership/mode semantics and atomic-rename durability behavior MUST remain supported. Directory-source publication/removal/pruning and output/manifest transactions remain separate; no uncooperative-writer exclusion or new processing budgets are introduced.

#### Scenario: Portable file publication refuses replaced leaf generation

- **WHEN** an admitted portable native file update encounters a replaced leaf or retargeted parent after staging
- **THEN** materialization MUST report a coded refusal, retain admitted/replacement/foreign/manifest bytes and clean only attempt-owned staging, while valid content updates still publish

### Requirement: Portable directory publication validates authority before live removal

Agent Marketplace portable directory publication MUST stage source contents through captured source readers and captured destination parent authority before live output removal. Opened source modes MUST remain faithful on staged files. Full observed root generation, saved ownership digest, named parent and staged object authority MUST be rechecked before descriptor-relative removal/rename. Refusal before live removal MUST preserve admitted/replacement/manifest bytes and clean only attempt-owned staged directories. Valid native updates/no-op/migration controls MUST remain supported. Pruning, aggregate/restore/ownership consumers and output/manifest transactions remain separate; directory removal/rename are not transactional and do not exclude uncooperative writers. No new arbitrary processing budget or fsync policy is introduced.

#### Scenario: Portable tree publication preserves replacement generation

- **WHEN** an admitted portable native tree update encounters a replacement tree after complete source staging
- **THEN** materialization MUST report a coded refusal, retain admitted/replacement/manifest bytes and clean staged residue, while valid tree content updates continue publishing

### Requirement: Portable pruning revalidates admitted entry and parent authority

Agent Marketplace manifest-driven portable pruning MUST retain captured destination parents and revalidate full observed entry state, recorded ownership digest and named parent object before descriptor-relative removal. A replaced entry or retargeted parent MUST retain coded refusal and MUST preserve admitted/replacement/manifest bytes. Stale portable links MUST be revalidated as managed symlink objects without reading their targets, and refused removals MUST NOT be reported as successful. Empty-parent cleanup MUST retain strict captured ancestor authority and use kernel-enforced empty-directory-only removal, never remove a regular file or follow an intermediate link. Valid pruning, dry-run and best-effort empty cleanup MUST remain supported. No new arbitrary budget, transactional prune or uncooperative-writer exclusion is introduced.

#### Scenario: Portable pruning refuses replacement generation

- **WHEN** manifest-authorized native portable pruning encounters a replaced output after ownership classification
- **THEN** materialization MUST report refusal without removing replacement/admitted bytes or changing manifest bytes, while healthy pruning remains functional

### Requirement: Native managed-link pruning retains captured removal authority

Agent Marketplace native skill/resource-group and artifact-link pruning MUST remove stale managed symlinks through captured authorized parent descriptors, revalidating full classified entry state, managed-cache target text and named parent before relative removal without reading target bytes. A replacement regular file or retargeted parent MUST retain coded refusal rather than being removed. Valid stale-link pruning, selected siblings, user-owned preservation and dry-run behavior MUST remain supported; artifact empty-parent cleanup MUST retain strict captured empty-directory-only authority. Marker-bearing regular-file cleanup and metadata discovery remain separate. No arbitrary new budget, transactional prune or exclusion of uncooperative writers is introduced.

#### Scenario: Native managed-link pruning refuses replacement generation

- **WHEN** native stale-link pruning encounters a replacement after classifying a managed symlink while a valid sibling remains selected
- **THEN** materialization MUST report refusal and retain replacement bytes, while healthy native stale-link and sibling controls continue working

### Requirement: Native ownership marker payloads validate opened-file authority

Agent Marketplace native leading-frontmatter ownership marker checks MUST read only through captured regular, singly linked and nonblocking opened-file authority, bounded by captured size with read-state/named-target validation. Final symbolic marker leaves MUST NOT be followed. Unsafe or unreadable payloads and cleanup failures MUST NOT grant ownership; existing bool/no-ownership and coded user-owned collision preservation semantics MUST remain supported. Valid native markers MUST retain leading-frontmatter interpretation and payload/mode/no-op semantics without new arbitrary size limits. Later publication/prune generations remain separate contracts.

#### Scenario: Native ownership marker refusal preserves output

- **WHEN** native command update encounters a multiply linked or FIFO marker payload after a valid native marker installation
- **THEN** ownership checks MUST complete without accepting the claim or blocking and preserve native/retained bytes through the existing coded collision contract, while valid markers continue working

### Requirement: Native command publication binds authorization to captured generation

Agent Marketplace native command-file publication MUST stage compiled bytes below captured authorized parents and revalidate the observed destination generation, named parent and attempt-created staged file before descriptor-relative rename. Admitted legacy managed links MUST NOT be unlinked before staging. Opened native equality reads MUST retain captured regular/single-link/nonblocking file authority rather than reopening a checked path; read failures MUST NOT authorize overwrite. Existing permissions MUST survive content updates, and identical repeats MUST retain identity/mode/mtime. Refusal before rename MUST preserve original/replacement bytes and clean only attempt-owned staging. No new arbitrary size policy, fsync policy, transaction or uncooperative-writer exclusion is introduced.

#### Scenario: Native command publication refuses replacement generation

- **WHEN** a valid native command update encounters a replacement file after staging and ownership classification
- **THEN** materialization MUST report coded refusal, preserve replacement/original bytes without attempt-owned staging residue, and keep valid content-update/mode/no-op controls supported

### Requirement: Native marker-file pruning revalidates removal authority

Agent Marketplace grouped command, uncovered file-copy and legacy regular-file resource-group pruning MUST retain captured authorized parent descriptors and revalidate full observed entry state, refreshed scoped ownership digest and named parent before descriptor-relative removal. A replacement MUST retain coded refusal and preserve replacement/original bytes. Symbolic leaves MUST retain managed-cache link authority without target reads. Valid grouped/uncovered removal, selected siblings, user-owned preservation and dry-run semantics MUST remain supported; refused removals MUST NOT be reported successful. Directory discovery remains separate. No new arbitrary budget, transaction or exclusion of uncooperative writers is introduced.

#### Scenario: Native command pruning preserves replacement

- **WHEN** grouped or uncovered native command cleanup encounters a replacement after marking an installed command stale
- **THEN** materialization MUST refuse with a coded error and preserve replacement/original bytes, while valid grouped/uncovered stale-command removal continues working

### Requirement: Prune directory discovery retains captured authority

Agent Marketplace skill/resource-group, command-file and artifact-link prune discovery MUST enumerate below captured authorized output-directory authority, revalidating the named directory generation before enumeration. Retargeted roots MUST refuse before importing foreign member names or payload ownership claims into native reports. Artifact-link classification MUST read link text descriptor-relatively without reading target payloads. Real harness authority MUST reach marker readers. Genuine optional absence MAY remain empty; unsafe or generation refusal MUST NOT become successful absence, including dry-run. Valid pruning/no-op/user-owned preservation behavior MUST remain supported without arbitrary new limits, mutable-tree transaction or exclusion of uncooperative writers.

#### Scenario: Prune directory refusal precedes foreign discovery

- **WHEN** native installed-resource repeat encounters a retargeted prune directory after admission
- **THEN** materialization MUST refuse with a coded error before importing foreign members, retain admitted/native/foreign bytes and keep healthy discovery/pruning working

### Requirement: Native marker-based aggregate reconciliation retains captured authority

Agent Marketplace marker-based native aggregate reconciliation MUST read payload and ownership claims through strict captured regular, singly linked, nonblocking opened-file snapshots bound to the observed generation. Genuine absence MAY initialize an empty aggregate; unsafe/generation errors MUST remain refusals. Publication MUST retain captured authorized parent and full observed destination/staged identity rechecks before descriptor-relative rename. Existing shared modes, JSONC comments, handwritten/unrelated fields, private new creation and identical-output identity/mode/mtime MUST remain supported. Refusal MUST preserve admitted/replacement bytes and clean only attempt-owned staging. No new arbitrary thresholds, fsync policy, full-output transaction or uncooperative-writer exclusion is introduced; durable-ownership adapter consumers remain separate.

#### Scenario: Native marker aggregate refuses replacement

- **WHEN** repository or machine marker-based native MCP update encounters a replacement aggregate after a valid native install and content update
- **THEN** materialization MUST refuse with a coded error and preserve admitted/replacement bytes while valid mode/comment/no-op controls continue working

### Requirement: Durable native aggregate readers stay bound to canonical target roots

Agent Marketplace durable native aggregate reconciliation and repair MUST use the ownership store's scoped captured reader rather than reopening a lexical aggregate pathname. A target-root retarget after store/session admission MUST NOT import a foreign aggregate as a healthy no-op. Valid native installation, unchanged repeats, root aliases admitted canonically, private creation, handwritten settings and durable ownership semantics MUST remain supported. These reader controls do not close publication or journal/state consumers.

#### Scenario: Durable aggregate reader retains canonical root

- **WHEN** native Gemini aggregate repeat encounters a target-root retarget after an actual installed-generation and ownership-session acquisition
- **THEN** materialization MUST report refusal instead of foreign no-op success and preserve original and foreign aggregate bytes

### Requirement: Skill support pruning remains within its captured cache namespace

Agent Marketplace MUST capture the skill-cache directory before discovering support-prune members and MUST perform discovery and removals through that captured directory. The named cache MUST still denote the admitted object before discovery and each removal. Retarget MUST refuse with materialize.write.error before deleting foreign members or installing the refused native link. Compiled artifacts and their required directories MUST remain preserved. Fully managed support reconciliation and initially supported selected root aliases remain compatible. This requirement does not make cache compilation, support copying and native publication a whole-tree transaction.

#### Scenario: Support cache prune refuses retargeted generation

- **WHEN** public native sync replaces the cache with a foreign alias at the support-prune boundary after compiled cache creation
- **THEN** pruning MUST refuse without changing foreign bytes or inode/mode/mtime, publishing the native link or leaving attempt support staging, and restoring the original cache MUST permit real support installation with its mode and deletion of obsolete support
