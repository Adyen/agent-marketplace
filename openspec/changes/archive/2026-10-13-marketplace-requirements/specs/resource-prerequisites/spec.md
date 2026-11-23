## ADDED Requirements

### Requirement: Marketplace prerequisite lookup uses the configured Marketplace

For an unmet prerequisite declared by a Marketplace-selected resource, Agent Marketplace SHALL look up candidates only in the requiring resource's configured Marketplace entry. Without a registry hint, it SHALL use the requiring resource's registry. With a registry hint, it SHALL use a registry bound in that entry whose registry slug or registry ID equals the hint. A hint with no bound match SHALL remain unmet with reason `source-not-configured`, without fallback to the requiring registry or acquisition of another source. When the requiring pin records a registry commit and the target registry is the requiring registry, lookup SHALL consult only that exact registry occurrence and its verified registry revision manifest. If the Marketplace does not serve that occurrence, or the resource is absent from it, the prerequisite SHALL remain unmet with reason `not-found`, without falling back to the current revision. Otherwise lookup SHALL use the target registry's current revision. A `path` hint SHALL match only when the candidate's source root, the single root of its exact registry occurrence containing its source path, equals the normalized hint. When the source root cannot be established, the candidate SHALL be unresolved with reason `not-found`. An effective exact Marketplace exclusion SHALL give reason `excluded`. Offline resolution MUST NOT contact a Marketplace for lookup. Lookup failures other than an unserved occurrence or an unavailable resource SHALL surface through the existing Marketplace resolution error path. Git and directory prerequisite lookup SHALL remain unchanged.

#### Scenario: Unhinted Marketplace prerequisite uses the requiring registry

- **WHEN** an unmet prerequisite of a Marketplace resource has no hint and a matching resource exists only in another registry
- **THEN** lookup MUST NOT use the other registry, and the prerequisite MUST remain unmet with reason `not-found`

#### Scenario: Unbound Marketplace registry hint has no fallback

- **WHEN** a Marketplace prerequisite's registry hint names no registry bound in the requiring Marketplace entry
- **THEN** the prerequisite MUST remain unmet with reason `source-not-configured`
- **AND** no Marketplace request MUST be made for that prerequisite

#### Scenario: Registry commit lookup does not fall back to the current revision

- **WHEN** the requiring pin records a registry commit whose occurrence the Marketplace does not serve, or which lacks the prerequisite
- **THEN** the prerequisite MUST remain unmet with reason `not-found`
- **AND** the current revision MUST NOT be used

#### Scenario: Unestablished source root leaves a path hint unresolved

- **WHEN** a Marketplace prerequisite has a path hint and the candidate's source root cannot be established or differs from the hint
- **THEN** the prerequisite MUST remain unmet with reason `not-found`

#### Scenario: Offline resolution makes no lookup request

- **WHEN** resolution runs offline with an unmet Marketplace prerequisite
- **THEN** no Marketplace request MUST be made for it

### Requirement: Requires-held Marketplace pins are implied, never user-selected

A Marketplace resource that the acquisition lock holds only through requires edges SHALL NOT be a user selection. It SHALL be matched through a requires edge whose parent is selected and still declares a prerequisite with its kind and name. When expansion is enabled, it SHALL be selected as an implied resource with prerequisite provenance naming each such parent. When expansion is disabled, it SHALL appear in the implied preview and SHALL NOT be selected. A requires-held resource with no such parent SHALL be neither selected nor previewed. Requires provenance MUST NOT be reported as collection provenance. Lock-to-configuration admission SHALL treat a resource reachable through requires edges from a reachable parent as reachable.

#### Scenario: Enabled expansion selects a requires-held pin as implied

- **WHEN** expansion is enabled and a selected Marketplace resource still requires a resource that the lock holds only through its requires edge
- **THEN** that resource MUST be selected as an implied resource whose prerequisite provenance names the parent

#### Scenario: Disabled expansion previews a requires-held pin

- **WHEN** expansion is disabled and the lock holds a resource only through a requires edge of a selected resource that still requires it
- **THEN** the resource MUST appear in the implied preview and MUST NOT be selected

#### Scenario: Orphaned requires-held pin is ignored

- **WHEN** no selected parent of a requires-held resource still declares its prerequisite
- **THEN** the resource MUST be neither selected nor previewed

#### Scenario: Requires provenance is not collection provenance

- **WHEN** a requires-held resource is reported
- **THEN** it MUST NOT be reported as a collection member

### Requirement: Release N does not record Marketplace prerequisites

Agent Marketplace SHALL NOT write a requires edge or create a Marketplace prerequisite pin. A lookup candidate SHALL appear in the implied preview, but it MUST NOT preserve materialized outputs or block their pruning. When expansion is enabled, the prerequisite SHALL remain unmet with reason `unrecorded`. Every acquisition-lock rewrite SHALL keep requires edges whose parent remains locked with the same content digest, drop requires edges whose parent is gone or has a different digest, and prune records that only dropped edges held. Lookup MUST NOT change configuration or acquisition-lock bytes.

#### Scenario: Lookup candidate is previewed, not recorded

- **WHEN** lookup finds a candidate for an unmet Marketplace prerequisite
- **THEN** the candidate MUST appear in the implied preview
- **AND** neither configuration nor the acquisition lock MUST change
- **AND** the candidate MUST NOT preserve or block pruning of any materialized output

#### Scenario: Enabled expansion reports an unrecorded candidate

- **WHEN** expansion is enabled and lookup finds a candidate for an unmet Marketplace prerequisite
- **THEN** the prerequisite MUST remain unmet with reason `unrecorded`

#### Scenario: Updating a requiring resource prunes its stale requires-held pins

- **WHEN** a requiring resource is updated to a new content digest and the lock holds another resource only through its requires edge
- **THEN** the rewritten lock MUST drop that edge and the record it alone held
- **AND** it MUST NOT contain any new requires edge

#### Scenario: Unrelated lock rewrites keep valid requires edges

- **WHEN** the lock is rewritten for an unrelated install, removal, or sync while a requires edge's parent stays locked with the same digest
- **THEN** the rewritten lock MUST keep that edge and the record it holds

## MODIFIED Requirements

### Requirement: Unmet prerequisite warnings carry typed reasons

Each unmet prerequisite warning SHALL carry exactly one typed reason: `not-found`, `excluded`, `source-not-configured`, or `unrecorded`. The reason SHALL distinguish an absent candidate, a candidate prevented by an effective exclusion, a source hint with no configured match, and a Marketplace candidate that was found but could not be recorded. Warning text, credential redaction, and shell-literal remediation SHALL retain their existing requirements.

#### Scenario: Missing candidate has not-found reason

- **WHEN** lookup finds no eligible candidate for an unmet prerequisite and no source hint is unconfigured
- **THEN** its warning MUST carry reason `not-found`

#### Scenario: Excluded candidate has excluded reason

- **WHEN** a matching candidate is prevented from selection by an effective explicit exclusion or exclude-all selection
- **THEN** its warning MUST carry reason `excluded`

#### Scenario: Unconfigured hint has source-not-configured reason

- **WHEN** a prerequisite's source hint matches no configured source
- **THEN** its warning MUST carry reason `source-not-configured`

#### Scenario: Unrecordable Marketplace candidate has unrecorded reason

- **WHEN** expansion is enabled and a Marketplace lookup candidate exists but cannot be recorded
- **THEN** its warning MUST carry reason `unrecorded`
