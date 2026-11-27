# Spec Delta

## ADDED Requirements

### Requirement: Place prerequisite graphs are bound by exact locks

A Place exact lock SHALL bind the canonical prerequisite graph separately from the payload archive digest. Online and cached delivery MUST use only the graph bound by that lock; missing historical evidence MUST NOT be inferred from live Marketplace data or treated as an empty graph.

#### Scenario: Same payload retains distinct Place prerequisite graphs

- **WHEN** identical payload bytes are published with an empty prerequisite graph and then with a non-empty graph
- **THEN** both publications MUST succeed without changing the payload archive digest, and each exact Place lock MUST bind its own canonical graph

#### Scenario: Online and cached locks retain original prerequisite graphs

- **WHEN** clients acquire the older and newer exact Place locks online or from cache
- **THEN** each client MUST use the graph bound by its selected lock, without adopting a later live graph

#### Scenario: Missing historical prerequisite binding refuses

- **WHEN** an exact historical Place lock has no prerequisite binding and no exact original graph evidence is available
- **THEN** online and cached delivery MUST refuse without inferring an empty graph or adopting the current graph

### Requirement: Scoped Place delivery refuses revoked revisions

Scoped Place commands MUST reject `--allow-revoked` with an unsupported-option diagnostic naming that flag. Direct and collection-selected Place locks MUST also refuse revoked revisions online and from cache. This MUST NOT weaken assessment, availability, or archive-integrity checks, or change ordinary non-Place revocation policy.

#### Scenario: Unsupported Place revocation option names the flag

- **WHEN** a scoped Place command receives `--allow-revoked`
- **THEN** it MUST refuse with an unsupported-option diagnostic naming `--allow-revoked` before acquisition or writes

#### Scenario: Place locks refuse revoked revisions online and from cache

- **WHEN** a direct or collection-selected Place lock resolves to a revoked revision online or from cache
- **THEN** the Place operation MUST refuse and leave configuration, locks, ownership, and target files unchanged

### Requirement: Bounded Place YAML preserves accepted mapping syntax

Place profile and package YAML within existing byte limits MUST accept valid nested mappings regardless of mapping width. Duplicate keys, unknown fields, and invalid scalars MUST remain refusals. Reserved-key refusals apply only to keys already reserved by the applicable schema; previously accepted keys MUST remain accepted.

#### Scenario: Wide nested mappings remain valid on both Place parse paths

- **WHEN** a profile document and a package document each contain wide, valid nested mappings within their byte limits
- **THEN** both documents MUST be accepted without narrowing the supported mapping syntax

#### Scenario: Strict Place YAML validation remains enforced

- **WHEN** either Place document contains a duplicate key, unknown field, invalid scalar, or reserved-key collision
- **THEN** the document MUST be refused

#### Scenario: Keys not reserved by existing schemas remain accepted

- **WHEN** a profile or package document uses a key accepted by its existing schema that is not reserved there
- **THEN** the document MUST remain accepted

### Requirement: One verified commit has separate bounded acquisition views

Legacy resource discovery and Place discovery SHALL use separate views of one verified Git commit, with acquisition limits enforced in aggregate. Legacy link dependencies and refusal of required broken legacy links MUST remain effective. A legacy link overlapping selected Place paths MUST refuse Place acquisition. Unrelated dangling links outside both views MUST be ignored.

#### Scenario: Mixed legacy and Place views succeed independently

- **WHEN** one verified commit contains valid legacy and Place selections, a valid legacy link dependency, and unrelated dangling links outside both views
- **THEN** both views MUST succeed independently, the legacy dependency MUST be preserved, and unrelated dangling links MUST NOT affect acquisition

#### Scenario: A required broken legacy link still refuses

- **WHEN** a required legacy link dependency in the legacy view is broken
- **THEN** legacy acquisition MUST refuse under its existing link-validation behavior

#### Scenario: A legacy link overlapping Place selection refuses

- **WHEN** a legacy link dependency overlaps a selected Place path in one verified commit
- **THEN** Place acquisition MUST refuse without partially publishing the Place selection

#### Scenario: Aggregate acquisition overflow refuses

- **WHEN** the combined legacy and Place views exceed an existing aggregate acquisition limit
- **THEN** acquisition MUST refuse before publication

### Requirement: Place generations are isolated from historical storage and collection

Place generation and ownership state MUST be isolated from historical package-generation writers and garbage collection. Pending and committed Place generation references MUST remain distinguishable until transaction commit.

#### Scenario: Historical native writes preserve Place ownership and generation

- **WHEN** a historical native writer changes legacy-owned state in an isolated fixture that also contains pending or committed Place evidence
- **THEN** Place ownership bytes and generation bytes MUST remain unchanged

#### Scenario: Historical garbage collection preserves Place ownership and generation

- **WHEN** historical garbage collection scans legacy-owned state in an isolated fixture that also contains pending or committed Place evidence
- **THEN** Place ownership bytes and generation bytes MUST remain unchanged, and pending and committed references MUST remain distinct

### Requirement: Place directory use requires creation provenance

Place delivery MUST establish creation provenance before claiming, reusing, or pruning a directory as transaction-created. If provenance is uncertain, it MUST refuse and preserve the directory, related files, ownership state, and transaction evidence.

#### Scenario: Uncertain directory provenance keeps evidence

- **WHEN** Place recovery or pruning cannot establish that a directory was created by the recorded transaction
- **THEN** it MUST refuse without claiming or removing the directory and MUST retain related files and evidence

### Requirement: Place probe and check launches bind validated objects

An initial-load probe MUST bind the validated working root; an initial-load or target-verify check MUST additionally bind its exact validated subject. A path spelling change alone MUST NOT require refusal when execution remains bound to the same validated objects. If a root or subject binding cannot be proven, the command MUST refuse before running host code.

#### Scenario: Probe binds its working root without a subject

- **WHEN** an initial-load probe runs after validating its working root and executable
- **THEN** it MUST use the validated working root and MUST NOT require a check subject

#### Scenario: Checks bind the root and exact subject

- **WHEN** an initial-load or target-verify check launches
- **THEN** it MUST use the validated working root and exact validated subject

#### Scenario: Path spelling change preserves proven location binding

- **WHEN** a root or subject pathname changes but the runner remains bound to the same validated object
- **THEN** the pathname change alone MUST NOT require refusal

#### Scenario: Unbound root or subject refuses before launch

- **WHEN** the runner cannot preserve the validated root or exact subject binding
- **THEN** it MUST refuse before running host code or recording a result

### Requirement: Place checks execute approved bytes or refuse

Initial-load probes and checks, and `target verify`, MUST execute the approved bytes for scripts and native executables. A pathname change alone MUST NOT require refusal when the runner remains bound to the approved executable object. If approved bytes cannot be guaranteed at launch, the command MUST refuse before running host code.

#### Scenario: Path change preserves proven executable binding

- **WHEN** an executable pathname changes after approval but the runner remains bound to the approved executable object
- **THEN** execution MUST use only the approved bytes, and the pathname change alone MUST NOT require refusal

#### Scenario: Changed approved executable bytes refuse before launch

- **WHEN** a script or native executable changes in place after approval so the approved bytes cannot be guaranteed at launch
- **THEN** the command MUST refuse before running that executable

## MODIFIED Requirements

### Requirement: Initial-load verification results are recorded per generation

Sync SHALL run pending initial-load checks only after all approved Place work admitted to the invocation has committed ownership and completed pruning. Selected targets skipped as unusable are not admitted and MUST NOT block the barrier. Each eligible unchanged generation MUST bind its validated root, approved executable bytes, and exact subject, or refuse before launch. Once the journal clears, sync MUST record each completed check's result in one ownership-state update against its exact profile, generation, check identity, and host version, retaining pass/fail evidence if another check fails. A committed generation without a result remains verification-pending.

#### Scenario: Crash recovery leaves verification pending, distinct from edits

- **WHEN** recovery completes a pending publication, and separately a user later edits an installed file
- **THEN** recovery MUST leave verification pending, and status MUST report the edit as drift, without labeling either event as the other

#### Scenario: A skipped unusable target does not block the admitted-work barrier

- **WHEN** a collection skips an unusable target while all approved Place work admitted to the invocation commits and prunes
- **THEN** pending checks for admitted work MUST run after that work completes without waiting for the skipped target

#### Scenario: Completed check outcomes preserve pass and fail evidence

- **WHEN** all admitted Place work commits and prunes and one pending check passes while another fails
- **THEN** the pass and fail results MUST both be recorded and neither result MUST erase the other

### Requirement: Failed verification keeps files and retries

A failed check SHALL keep the placed projection and its generation reference and MUST NOT roll files back. The next mutating sync MUST retry each eligible failed or pending check only when its recorded projection is unchanged and after all approved Place work admitted to that invocation has committed and pruned. A later user edit MUST be reported as drift, not as a failed check. Recovery and read-only commands MUST run no host code, and read-only commands MUST report recorded results.

#### Scenario: Failed verification keeps files and retries

- **WHEN** an approved check fails after placement and a later mutating sync sees unchanged files and commits and prunes all approved Place work admitted to that invocation
- **THEN** the files and ownership MUST remain, status MUST report the failure without running code, and the later sync MUST retry the check exactly once after all approved Place work admitted to that invocation has committed and pruned

#### Scenario: A failed Place root defers checks for admitted work

- **WHEN** Place root A has a pending initial-load check while admitted Place root B fails placement on two successive mutating syncs, then a later sync commits and prunes all admitted work with A unchanged
- **THEN** neither failed invocation MUST run host commands, and the successful invocation MUST run A's pending check exactly once after all admitted work has committed and pruned

### Requirement: Target verify runs only the named approved check

`target verify <locator> --installation <installation-id>` SHALL run only the named approved check and record exact revisions, host-version status, the result and its evidence source, without installing, activating or repairing anything. When package files have drifted, it MUST report the drift and refuse without running a check or writing anything. Before launch it MUST bind the validated working root, approved executable bytes, and exact validated check subject; a pathname change alone MUST NOT require refusal if those bindings remain intact, and unprovable bindings MUST refuse before host code.

#### Scenario: Target verify refuses drifted files without running checks

- **WHEN** `target verify` runs for an installation whose files include an edit
- **THEN** it MUST report the edited path as drift and refuse, run no host command, and write no result, config, lock, approval, ownership or target file

#### Scenario: Target verify runs only the named check

- **WHEN** `target verify <locator> --installation <id>` runs for an unchanged installation with provable root, executable, and subject bindings
- **THEN** it MUST run only that approved check, record its exact revisions, host-version status, result and evidence source, and install, activate or repair nothing

#### Scenario: Target verify refuses when execution bindings cannot be proven

- **WHEN** `target verify` cannot preserve the validated root, approved executable bytes, or exact subject binding
- **THEN** it MUST refuse before running host code or recording a result
