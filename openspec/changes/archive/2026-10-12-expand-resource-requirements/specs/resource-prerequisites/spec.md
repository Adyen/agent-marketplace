## MODIFIED Requirements

### Requirement: Prerequisites do not change selection

When requirement expansion is unset or disabled, Agent Marketplace SHALL treat prerequisite declarations as advisory and SHALL NOT change resource selection because of them. When expansion is enabled, Agent Marketplace SHALL select configured, resolvable prerequisites transitively after collection expansion and required-shadow handling. An unavailable or explicitly excluded prerequisite MUST remain unmet without removing its requiring resource. Implied resources MUST NOT become required selections.

#### Scenario: Install finds an unmet prerequisite

- **WHEN** install selects a resource whose prerequisite remains unmet after resolution, because expansion is disabled or no eligible candidate can be selected
- **THEN** install MUST emit a remediable warning
- **AND** it MUST NOT select the unmet prerequisite

#### Scenario: Prerequisite is explicitly excluded

- **WHEN** a selected resource requires an explicitly excluded resource that is not `required: true`
- **THEN** the marketplace MUST report the unmet prerequisite
- **AND** it MUST preserve the explicit exclude and the requiring resource's selection

#### Scenario: Excluded prerequisite is required

- **WHEN** a selected resource requires an excluded resource provided by a `required: true` entry
- **THEN** existing `required: true` precedence MUST keep the prerequisite selected
- **AND** the marketplace MUST NOT report it as unmet

### Requirement: Prerequisite exit status

Unmet prerequisites SHALL NOT change the exit status of ordinary `install`, `sync`, `sync --check`, `status`, or `doctor`. Under `sync --strict`, including `sync --strict --check`, a prerequisite that remains unmet after resolution SHALL cause a nonzero exit status without changing resource selection. A prerequisite selected by enabled expansion SHALL NOT cause strict sync to fail.

#### Scenario: Commands succeed with unmet prerequisites

- **WHEN** install, sync, sync --check, status, or doctor finds only unmet prerequisites and nothing else to report
- **THEN** the command MUST exit with status 0

#### Scenario: Strict sync rejects unmet prerequisites

- **WHEN** `sync --strict` or `sync --strict --check` finds an unmet prerequisite
- **THEN** sync MUST print the remediable warning and MUST exit nonzero
- **AND** it MUST NOT select the unavailable or explicitly excluded prerequisite

#### Scenario: Strict sync succeeds when satisfied

- **WHEN** a selected resource's prerequisites are selected, including by enabled expansion, and `sync --strict` runs
- **THEN** sync MUST NOT print an unmet-prerequisite warning or fail because of it

## ADDED Requirements

### Requirement: Prerequisite lookup uses configured source identity

Agent Marketplace SHALL resolve an unmet prerequisite only from the requiring resource's exact source identity when it has no source hint. A Git `registry` hint SHALL identify an already configured source by normalized Git URL; an optional `path` hint SHALL narrow lookup to that configured root. A hint with no configured match SHALL remain unmet without fallback to the requiring resource's source or acquisition of another source. A directory hint SHALL match only an absolute, cleaned location of an already configured directory source. Lookup SHALL use already discovered catalog resources. A malformed candidate MUST retain its typed malformed diagnostic attributed to the requiring resource.

#### Scenario: Unhinted prerequisite uses the requiring source root

- **WHEN** an unmet prerequisite has no source hint and a matching resource exists only at a different URL, ref, or root
- **THEN** lookup MUST leave the prerequisite unmet and MUST NOT select the resource from the other source identity

#### Scenario: Configured Git hint narrows lookup to its root

- **WHEN** an unmet prerequisite has a Git hint matching an already configured source and a path naming one of its configured roots
- **THEN** lookup MUST consider matching resources from that root without acquiring or adding a source

#### Scenario: Unconfigured Git hint has no fallback

- **WHEN** an unmet prerequisite has a Git hint that matches no configured source and a same-named resource exists in the requiring resource's source
- **THEN** the prerequisite MUST remain unmet with reason `source-not-configured`
- **AND** the requiring resource's source MUST NOT be used as a fallback

#### Scenario: Directory hints require an absolute configured location

- **WHEN** a directory hint is compared with configured directory sources
- **THEN** only an absolute, cleaned configured directory location MUST match
- **AND** a relative path or source-name alias MUST NOT match a directory source

#### Scenario: Malformed candidate keeps its diagnostic attribution

- **WHEN** a candidate for an unmet prerequisite cannot be used because of malformed resource metadata
- **THEN** Agent Marketplace MUST retain its typed malformed diagnostic attributed to the requiring resource
- **AND** the requiring resource MUST remain selected

### Requirement: Expanded prerequisites obey scope and selection precedence

When expansion is enabled, Agent Marketplace SHALL admit a new implied resource separately in each scope containing a requiring resource, subject to the existing effective harness-target set. A prerequisite SHALL be satisfied by any resource actually selected in the same resolution with the same kind and name, regardless of source, configuration layer, install scope, or Marketplace selection; scope SHALL affect admission only for newly implied resources. An implied resource SHALL inherit the supplying source entry's targets intersected with the effective harness-target set. Explicit exclusions and exclude-all selections MUST prevent an implied selection unless an existing `required: true` selection supplies it, and a cross-scope exclusion MUST NOT change machine-scope selection. A direct selection MUST win over an implied selection with the same kind and name. Cycles MUST terminate, and deterministic resolved-resource and declaration order MUST determine the first implied winner.

#### Scenario: Transitive prerequisites are selected after collections

- **WHEN** expansion is enabled and a selected resource, including a collection member, requires a resource that itself has a resolvable prerequisite
- **THEN** Agent Marketplace MUST select the transitive prerequisite closure after collection expansion and required-shadow handling
- **AND** it MUST keep the original requiring resource selected

#### Scenario: Existing selection satisfies by kind and name

- **WHEN** any resource actually selected in the same resolution has the same kind and name as a prerequisite, regardless of source, configuration layer, install scope, or Marketplace selection
- **THEN** the prerequisite MUST be considered satisfied without selecting a duplicate

#### Scenario: Implied resources stay within scope and effective targets

- **WHEN** a requiring resource is selected in a scope with an effective harness-target set and its prerequisite is supplied by an entry with declared targets
- **THEN** the implied resource MUST be admitted only in that requiring scope
- **AND** its targets MUST be the intersection of the supplying entry's targets and the effective target set
- **AND** it MUST NOT become a `required: true` selection

#### Scenario: Cross-scope exclusion does not change machine selection

- **WHEN** an exclusion in one scope matches an implied prerequisite selected by a different scope
- **THEN** the exclusion MUST NOT remove or replace the other scope's selection

#### Scenario: Direct selection wins an implied collision

- **WHEN** a resource is selected directly and a prerequisite resolves to the same kind and name from another source
- **THEN** the direct selection MUST remain the winner

#### Scenario: Cycles and competing implied candidates resolve deterministically

- **WHEN** selected resources contain a prerequisite cycle or multiple declarations resolve to the same kind and name from different sources
- **THEN** resolution MUST terminate and the first candidate in resolved-resource and declaration order MUST win

### Requirement: Prerequisite provenance is distinct from collection provenance

Agent Marketplace SHALL retain which selected resources require each implied resource separately from collection membership. Prerequisite expansion MUST NOT be represented as collection provenance, and existing collection provenance MUST remain unchanged.

#### Scenario: Collection member requirement retains both relationships

- **WHEN** a selected collection member requires an implied resource
- **THEN** the result MUST identify the requiring collection member as the prerequisite source
- **AND** the implied resource's collection provenance MUST continue to identify only its collection membership

### Requirement: Disabled expansion reports an implied preview

When `requires.expand` is unset or false, Agent Marketplace SHALL compute the transitive closure of eligible prerequisites as an advisory preview without appending implied resources to the actual selected resources or writing implied selections to configuration. Existing selection, warning, and strict-sync semantics MUST remain unchanged. Repeated resolution of the same selections and catalog MUST produce the same preview without duplicate implied resources.

#### Scenario: Disabled expansion preserves selection while previewing prerequisites

- **WHEN** expansion is disabled and a selected resource has a resolvable prerequisite
- **THEN** the requiring resource MUST remain selected and the prerequisite MUST NOT be selected or written to configuration
- **AND** the advisory preview MUST contain the transitive implied resources

#### Scenario: Advisory preview is idempotent

- **WHEN** resolution is repeated with the same direct selections, catalogs, and configuration while expansion is disabled
- **THEN** it MUST produce the same implied preview and actual selection without duplicate implied resources

### Requirement: Unmet prerequisite warnings carry typed reasons

Each unmet prerequisite warning SHALL carry exactly one typed reason: `not-found`, `excluded`, or `source-not-configured`. The reason SHALL distinguish an absent candidate, a candidate prevented by an effective exclusion, and a source hint with no configured match. Warning text, credential redaction, and shell-literal remediation SHALL retain their existing requirements.

#### Scenario: Missing candidate has not-found reason

- **WHEN** lookup finds no eligible candidate for an unmet prerequisite and no source hint is unconfigured
- **THEN** its warning MUST carry reason `not-found`

#### Scenario: Excluded candidate has excluded reason

- **WHEN** a matching candidate is prevented from selection by an effective explicit exclusion or exclude-all selection
- **THEN** its warning MUST carry reason `excluded`

#### Scenario: Unconfigured hint has source-not-configured reason

- **WHEN** a prerequisite's source hint matches no configured source
- **THEN** its warning MUST carry reason `source-not-configured`

