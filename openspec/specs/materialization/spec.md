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

### Requirement: Skill support directory aliases remain scope-contained

Agent Marketplace MUST accept skill support directory aliases and alias chains whose resolution stays within the captured source root. Native installation and read-only portable checking MUST expand the same logical support tree, preserve regular singly-linked payload bytes and executable permissions, and exclude VCS internals at every level. Read-only checking MUST preserve output and source state, and unchanged file-mode output MUST remain a no-op. Escaping, dangling, cyclic, or unsafe-payload aliases MUST refuse with materialize.write.error without replacing an installed generation or importing external bytes. An alias that leaves the source root before returning to it MUST remain refused.

#### Scenario: Contained support directory aliases retain native install and check

- **WHEN** a skill with contained directory aliases, an alias chain, executable file aliases, and nested VCS internals is installed in either materialization mode and checked read-only
- **THEN** its native support files MUST be regular executable snapshots at each logical alias path, VCS internals MUST be absent, checking MUST report in sync without writes, and unchanged file-mode repetition MUST preserve native identity and modification time

#### Scenario: Unsafe support directory aliases preserve the installed generation

- **WHEN** an installed contained support directory alias becomes escaping, dangling, cyclic, leaves and returns to the root, or exposes multiply-linked payloads
- **THEN** read-only checking and native installation MUST report materialize.write.error while preserving installed and external bytes, identity, permissions, and modification time

### Requirement: Skill support directory aliases respect Git exclusions

Agent Marketplace MUST exclude support directory aliases whose contained resolved directory target is `.git` or a descendant of `.git`, even when the logical alias has another name. Real support copying and read-only portable checking MUST omit the same logical subtree while retaining ordinary contained directory aliases and executable file snapshots.

#### Scenario: Support directory aliases cannot expose VCS internals

- **WHEN** a skill has ordinary contained support aliases and aliases targeting `.git` and a nested `.git` subdirectory under different logical names
- **THEN** native installation in either materialization mode MUST retain ordinary executable support snapshots but omit both excluded alias subtrees, and read-only checking MUST report in sync without writes

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

### Requirement: Support-file publication retains admitted destination and staging generations

Agent Marketplace support snapshot publication MUST use the captured publisher for its classified destination generation. Parent, destination leaf and staged-file generation changes MUST refuse before rename, preserve replacement bytes and metadata, and remove only the attempt-created stage through its captured parent. Native sync MUST report materialize.write.error and MUST NOT install the refused cache-backed skill. Existing source mode and byte/mode no-op behavior remain compatible. This requirement does not make support-directory preparation, producer cleanup and native publication one transaction.

#### Scenario: Support publication refuses replaced parent and stage

- **WHEN** public native sync replaces the support destination parent after staging and places a foreign staged file at the same basename through the replacement alias
- **THEN** support publication MUST refuse, preserve foreign stage bytes/inode/mode/mtime/membership, clean its original stage without native installation, and restoring original parent MUST permit real byte-exact native support publication with its mode

### Requirement: Producer support staging cleanup retains admitted directory authority

Agent Marketplace MUST clean producer support staging through its captured parent and MUST require the named directory to retain its admitted object identity before recursive deletion. A replaced or unavailable producer generation MUST refuse cleanup without removing replacement bytes or metadata. Cleanup refusal MUST propagate through materialize.write.error before cache-backed native link installation. A healthy attempt MUST remove only its own staging directory. This requirement does not make earlier cache/support publication transactional or establish producer creation/write admission.

#### Scenario: Support producer cleanup preserves replaced stage directory

- **WHEN** public native sync replaces the producer staging directory with a different regular directory after support publication staging
- **THEN** cleanup MUST preserve foreign bytes/inode/mode/mtime and retained original support, refuse native installation, and a healthy retry MUST install actual source-mode support and clean only its new stage while preserving the previous replacement

### Requirement: Support preparation and writes share captured output authority

Agent Marketplace MUST retain one captured output directory for eligible support traversal. Descendant directory preparation, staged support file writes (created with exclusive create, never overwriting an existing name) and snapshot publication MUST remain relative to that authority, and directory capture MUST refuse a named-root generation change before mutation. Refusal MUST preserve foreign bytes/metadata/members and report materialize.write.error without native installation. Existing source modes and contained input aliases remain compatible; empty or excluded-only support MUST NOT create a missing output. This requirement does not establish an atomic cache/native transaction or producer namespace creation authority before copy admission.

#### Scenario: Support output refuses retarget before directory and file writes

- **WHEN** public native sync retargets admitted producer output before support traversal
- **THEN** preparation/writes MUST refuse without creating foreign directories/files or changing foreign bytes/inode/mode/mtime, keep native absent, and a healthy retry MUST actually create nested native support with exact source bytes/mode and clean its new staging

### Requirement: Cache namespace preparation retains admitted parent authority

Agent Marketplace MUST retain the selected cache parent directory during producer and compiled-directory namespace preparation. Creation MUST remain descriptor-relative and MUST refuse a named parent generation change before mutation. Private producer directories MUST start with mode0700 and their existing cleanup MUST retain admitted inode authority. Refusal MUST preserve foreign members/bytes/metadata and report materialize.write.error without installing the refused native link. Existing selected initial parent aliases remain compatible. This requirement does not make compilation/support/native publication a whole-tree transaction.

#### Scenario: Cache namespace creation refuses retargeted parent

- **WHEN** public native sync retargets the admitted cache parent before producer directory creation
- **THEN** namespace preparation MUST refuse without changing foreign members/bytes/inode/mode/mtime or leaving attempt members, keep native absent, and selecting an initial alias to the original parent MUST permit real source-mode native installation and own-staging cleanup

### Requirement: SDK artifact comparisons read admitted regular generations

SDK Materialize MUST compare artifact payloads through a captured parent and one opened singly linked regular file generation. Unsafe object shapes MUST refuse before bytes authorize a no-op; FIFO admission MUST NOT block. Classification and returned bytes MUST retain one full file state and captured-size growth checks, without introducing an absolute byte budget. Genuine absence, healthy byte comparisons, unchanged regular no-op identity/mode/mtime and real updates remain compatible. Comparison refusal MUST precede staging and preserve foreign bytes/metadata and output membership. This requirement does not fence SDK publication or stale discovery by itself.

#### Scenario: SDK output comparison refuses multiply linked cache file

- **WHEN** public SDK Materialize compares an existing regular artifact with another hardlink
- **THEN** it MUST refuse with the unsafe-file identity without staging or changing foreign bytes/inode/mode/mtime, and removing the extra link MUST permit unchanged no-op and a real source-mode payload update

### Requirement: SDK publication retains root and staging-directory authority

SDK Materialize MUST retain admitted output-root and attempt staging-directory objects through staged publication. Replacing either before publication MUST refuse with generation conflict before consuming foreign payloads. Destination parent creation and rename MUST remain descriptor-relative to captured output authority. Attempt cleanup MUST remove only its original directory inode, preserve replacement members/bytes/metadata and propagate refusal rather than report successful publication. Existing staged-all-artifacts behavior, modes, dry-run and partial-result semantics remain compatible. Initial stage preparation/writes and individual file-generation leases are not established by this requirement.

#### Scenario: SDK publication refuses replaced root and preserves foreign stage

- **WHEN** public SDK Materialize replaces output root after flat or nested staging and a foreign directory contains staged payload at the original basename
- **THEN** it MUST refuse before consuming foreign stage, preserve foreign bytes/inode/mode/mtime/membership, remove only original attempt stage, and healthy publication to original root MUST really write source-mode bytes without staging residue

### Requirement: SDK stage creation and writes retain captured directory authority

SDK Materialize MUST create output directories below captured selected ancestry and retain admitted output root before private stage creation. Named-root replacement MUST refuse before creating foreign stage members. Private staging MUST start with directory mode0700, match its original admitted directory object, and prepare/write files descriptor-relatively; existing-mode chmod MUST use the opened file. Existing umask/file modes, stage-all-artifacts, dry-run and partial-result semantics remain compatible. Refusal MUST preserve foreign bytes/metadata/membership and retain prior own-inode cleanup. This requirement does not establish individual staged/destination file-generation leases or stale discovery/removal.

#### Scenario: SDK stage creation refuses replaced output root

- **WHEN** public SDK Materialize replaces admitted output root immediately before stage creation
- **THEN** it MUST refuse generation conflict without creating foreign stage members or changing foreign bytes/inode/mode/mtime, leave original root without attempt members, and healthy nested source-mode publication MUST really write bytes and clean its stage

### Requirement: SDK file publication retains original destination state or absence

SDK Materialize MUST carry original opened artifact file identity/state and absence from comparison into publication. Full state or absence MUST remain unchanged before rename below captured output authority. Replacing an existing file or creating an originally absent leaf before publication MUST refuse generation conflict without changing replacement bytes/metadata or retained original and MUST clean only its attempt staging. Preserved merge modes MUST come from original opened file state. Existing healthy updates, source modes, no-op identity, staged-all-artifacts, dry/partial contracts remain compatible. This requirement does not retain original staged-file identity or fence stale discovery/removal by itself.

#### Scenario: SDK file publication preserves replaced destination generation

- **WHEN** public SDK Materialize replaces its admitted destination file or creates an originally absent destination after staging
- **THEN** publication MUST refuse without changing replacement bytes/inode/mode/mtime/member or retained original, clean its staging, and restoring original file or original absence MUST permit actual source-mode payload update

### Requirement: SDK staged-file publication and cleanup retain writer generations

SDK Materialize MUST retain original staged payload state from opened writer descriptors and revalidate full state before publication. Tracked staged-file replacement or state change MUST refuse generation conflict before rename. Attempt cleanup MUST validate remaining tracked staged files before removing its private directory, preserve changed/replacement payloads and propagate refusal. A replacement-bearing attempt stage MUST remain intact rather than be reported as successfully cleaned. Healthy actual source-mode publication MUST clean only its fresh stage while preserving prior replacement evidence. Existing staged-all-artifacts, modes/no-op/dry/partial contracts remain compatible; existing fully-owned staging-directory treatment of untracked members is unchanged.

#### Scenario: SDK staged file replacement preserves payload and refuses publication

- **WHEN** public SDK Materialize moves its completed staged payload aside and writes a foreign replacement below the same admitted staging directory
- **THEN** it MUST refuse before consuming/deleting replacement bytes/inode/mode/mtime, publish no destination, preserve original retained file and replacement-bearing output membership, and healthy retry MUST really publish source640 payload while preserving prior replacement evidence

### Requirement: SDK stale payload readers admit stable singly linked regular files

SDK stale-candidate digest reads MUST remain under captured parent authority, match original full file state and reject leaf aliases, nonregular or multiply linked payloads before hashing. Quarantine verification MUST open nonblocking and validate the opened regular singly linked file before reading; observed size and descriptor stability MUST bound all stale digests. Reader/close refusals MUST preserve original error identity, foreign payload bytes/metadata and existing quarantine restoration. Valid regular stale pruning and truthful removal reporting MUST remain functional. This requirement does not independently fence directory discovery, deletion generations or empty-parent cleanup.

#### Scenario: SDK stale reads refuse multiply linked payloads

- **WHEN** public SDK clean sync finds an independently authored stale regular file with an extra foreign hard link
- **THEN** it MUST refuse unsafe-file admission before staging or deletion, preserve both names/bytes/inode/mode/mtime/output membership, and removing the extra link MUST permit actual regular stale prune

### Requirement: SDK stale discovery retains original directory generations

SDK stale traversal MUST remain below captured original output and directory authority, match opened directories to original admitted metadata and validate original ancestry before/after importing descriptor-read members. A directory replacement MUST refuse generation conflict before importing its stale claims, including dry-run. Candidates MUST retain original no-follow leaf metadata/parent identities, preserve sorted paths/transaction exclusions/special-shape nondestruction and genuine absence behavior. Refusal MUST preserve replacement/original bytes/metadata/membership and create no staging. Valid restored directory discovery and actual pruning MUST remain functional. This requirement does not by itself fence subsequent deletion/reporting/empty-parent cleanup.

#### Scenario: SDK stale discovery refuses directory replacement before importing members

- **WHEN** public SDK sync or dry-run replaces an admitted nested real directory before member discovery
- **THEN** it MUST generation-refuse before importing foreign member claims, preserve foreign bytes/inode/mode/mtime/output membership and original retained payload, create no stage, and restoring original directory MUST permit actual regular prune or nonmutating dry-run candidate discovery

### Requirement: SDK quarantine publication and reservation cleanup retain creator authority

SDK stale pruning MUST retain the descriptor identity of each exclusively created private quarantine reservation, require original regular singly linked empty reservation before rename, and remove only matching attempt-created reservations during refusal cleanup. Unknown or replacement authority MUST be preserved with the existing stale-prune conflict; meaningful reader/close failures MUST not become success. Existing regular pruning and restoration behavior MUST remain functional. This requirement does not independently fence later quarantined-payload deletion/restoration, removal reporting or empty-parent cleanup.

#### Scenario: SDK quarantine reservation replacement preserves foreign payload

- **WHEN** public SDK sync replaces its actual private quarantine reservation before rename with an independently authored foreign regular payload
- **THEN** it MUST stale-conflict without consuming/deleting foreign bytes/inode/mode/mtime, preserve original retained empty reservation and selected stale bytes/output membership, clean only owned staging, and a healthy retry MUST actually prune selected stale payload while preserving the earlier transaction-prefixed replacement

### Requirement: SDK empty-parent cleanup retains admitted directory authority

SDK post-prune empty-parent cleanup MUST use captured output and parent descriptors, require the discovery-admitted parent/directory generations before directory-only removal, and propagate meaningful generation/inspection/close refusal instead of reporting successful cleanup. Unknown or replacement directories MUST be preserved. Nonempty directories MUST stop normal cleanup, and valid nested stale/empty-parent pruning MUST remain functional; the output root MUST never be removed.

#### Scenario: SDK empty-parent replacement preserves directory generation

- **WHEN** public SDK sync prunes a real nested stale payload and its admitted empty parent is replaced after initial inspection with an independently created foreign empty directory
- **THEN** SDK MUST generation-refuse without deleting/changing the foreign directory inode/mode/mtime or output membership, preserve the original retained now-empty parent, clean only owned staging, and restoring valid authority with a real stale payload MUST permit actual stale-file and empty-parent removal while foreign data stays unchanged

### Requirement: SDK quarantined-payload removal retains admitted payload authority

SDK MUST revalidate the discovery-admitted quarantined payload generation immediately before deletion, using captured parent authority and the existing bounded no-follow/nonblocking single-link reader. Changed or unknown payloads observed by final verification MUST return the existing stale-prune conflict, remain untouched by deletion/attempt cleanup and MUST NOT be restored as the admitted original stale file. Genuine stale pruning MUST remain functional. Verification and name-based deletion are separate operations, not an atomic identity-conditional deletion. Deliberate interference by another process running with the same OS user ID and write access to the quarantine directory during that final verification/deletion window is outside the supported threat model; private naming and repeated verification MUST NOT be represented as atomic hostile same-user exclusion or guaranteed preservation in that window. This narrow exclusion does not relax other generation, containment, ownership, restoration, cleanup/recovery or removal-reporting requirements. This rule does not independently fence the earlier conflict-restoration path or redefine removal reporting.

#### Scenario: SDK quarantined-payload replacement preserves removal generation

- **WHEN** public SDK sync verifies a genuinely quarantined stale payload and that name is replaced after initial verification but before final verification with independently authored foreign regular bytes
- **THEN** sync MUST stale-conflict and preserve foreign and retained original bytes/inodes/modes/mtimes, avoid unauthorized stale-name restoration, preserve output membership and clean only owned staging, and restoring valid authority MUST permit actual stale deletion while earlier transaction-prefixed foreign replacement stays unchanged

### Requirement: SDK quarantine restoration retains observed namespace authority

SDK MUST capture no-follow descriptor-relative identity of the actual quarantined entry before verification and retain that observed device/inode/mode/size/link shape through mismatch restoration and quarantine cleanup. Link creation MUST account only for its own link-count change. Unknown/later generations MUST remain untouched while the original stale-prune conflict remains visible; they MUST NOT be restored as the observed entry. Existing legitimate regular/FIFO/changed-leaf restoration and actual stale pruning MUST remain functional. This is not an atomic uncooperative-writer compare-and-act policy or a change to removal reporting.

#### Scenario: SDK quarantine restoration preserves observed generation

- **WHEN** public SDK sync observes a genuine quarantined mismatch B after discovery of A and B is replaced at the restore boundary with independently authored foreign C
- **THEN** sync MUST retain the original stale conflict and preserve foreign C and retained A/B bytes/inodes/modes/mtimes, avoid unauthorized stale-name restoration, retain output members and only owned-stage cleanup, and restoring valid authority MUST permit real healthy pruning while foreign quarantine C stays unchanged

### Requirement: SDK live removal reporting reflects actual deletion receipts

SDK live Removed entries MUST be backed by successful validated stale-payload unlink outcomes, not discovery/planning, conflict refusal, restored replacement, or candidate disappearance before mutation. Physical removal receipts MUST remain independent of later cleanup/close error. Public removal API, original conflict/error identities and complete sorted StaleCandidates plan MUST remain compatible. Dry-run Removed MUST retain prospective plan semantics without filesystem mutation.

#### Scenario: SDK removal receipts distinguish refusal absence and dry-run

- **WHEN** public SDK sync discovers sorted safe A and unsafe B and A is genuinely pruned while B is replaced/refused or disappears before rename
- **THEN** live Removed MUST contain only actual A while the complete A+B candidate plan remains, conflict/original/foreign bytes and metadata/output members/staging guarantees remain intact, dry-run MUST prospectively report A+B without mutation, and healthy controls MUST perform the exact deletion(s) their receipts claim

### Requirement: Repository Windsurf portable nominations stay within native output

Repository-scoped Windsurf portable manifest nominations MUST identify descendants of the native `.windsurf/` output namespace. The namespace root itself and unrelated repository paths MUST NOT authorize payload reads, replacement, or pruning through a matching ownership digest. Refused nominations MUST report `materialize.manifest.error`, preserve nominated bytes, identity, permissions, modification time and manifest evidence, and MUST NOT report the refused entry as removed. Genuine Windsurf portable installation and pruning, machine-scope output, and supported initial root and scope aliases MUST remain usable. This requirement does not authenticate manifest provenance, change ownership semantics within authorized output roots, or change SDK explicit-output pruning.

#### Scenario: Windsurf manifest cannot nominate unrelated repository files

- **WHEN** repository file-mode materialization encounters an independently authored Windsurf portable manifest nomination of the native namespace root or an unrelated repository file outside `.windsurf/`, with a matching ownership digest, in a mutating or read-only run
- **THEN** it MUST refuse with `materialize.manifest.error`, preserve the unrelated file bytes, identity, permissions, modification time and manifest evidence, report no removal for that entry, and continue to support actual installation and pruning of native Windsurf portable output
