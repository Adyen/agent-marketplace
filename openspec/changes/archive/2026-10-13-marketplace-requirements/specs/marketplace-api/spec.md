## ADDED Requirements

### Requirement: Resource detail exposes prerequisites

The latest resource detail response SHALL include `requires` exactly when the revision is indexed: an empty array when it declares none, and omitted when it is unindexed. Each entry SHALL carry the declared kind and name and the stored, credential-redacted `registry` and `path` hints. An entry SHALL include `resolved` with the target's registry slug, kind, and name only when all of the following hold. The target registry is the requiring registry when there is no registry hint, or otherwise the registry of the same Marketplace whose slug, or failing that whose ID, equals the hint. The target has a current-head occurrence. When a `path` hint is present, the target's source root equals the normalized hint. The target's own resource detail request would succeed for the same caller. The source root is the single root of the target's registry occurrence that contains the target's source path. When no single root qualifies, the source path is unknown, or the hint is absolute, escaping, or otherwise unsafe, the path hint is unmatched. A hidden target MUST be indistinguishable from a missing one. Search, revision-list, and revision-by-digest responses MUST NOT gain these fields.

#### Scenario: Indexed revision lists its prerequisites

- **WHEN** a caller requests the latest detail of an indexed resource that declares prerequisites
- **THEN** the response MUST list them in declaration order with their declared kinds, names, and redacted hints

#### Scenario: Indexed revision without prerequisites returns an empty list

- **WHEN** a caller requests the latest detail of an indexed resource that declares no prerequisites
- **THEN** `requires` MUST be an empty array

#### Scenario: Unindexed revision omits prerequisites

- **WHEN** a caller requests the latest detail of an unindexed revision
- **THEN** the response MUST omit `requires`

#### Scenario: Unhinted prerequisite resolves in the same registry

- **WHEN** a prerequisite has no registry hint and a visible resource with its kind and name is in the requiring registry's current head
- **THEN** its entry MUST include `resolved` naming that resource

#### Scenario: Registry hint resolves by slug within the same Marketplace

- **WHEN** a prerequisite's registry hint equals the slug or ID of another registry of the same Marketplace whose current head has a visible matching resource
- **THEN** its entry MUST include `resolved` naming that registry's resource

#### Scenario: Hidden or missing target is unresolved

- **WHEN** the matching target is absent, outside the current head, or would not be returned by its own detail request for the caller
- **THEN** the entry MUST omit `resolved` identically in each case

#### Scenario: Path hint must equal the derived source root

- **WHEN** a prerequisite's path hint equals the single occurrence root containing the target's source path
- **THEN** the entry MUST include `resolved`

#### Scenario: File prefix or ambiguous root does not satisfy a path hint

- **WHEN** a path hint names a directory prefix of the target's source path that is not its source root, or nested roots make the source root ambiguous, or the source path is unknown
- **THEN** the entry MUST omit `resolved`

#### Scenario: Search and revision responses are unchanged

- **WHEN** a caller searches resources, lists revisions, or requests a revision by digest
- **THEN** those responses MUST NOT include `requires` or `required_by`

### Requirement: Resource detail exposes dependents within the same Marketplace

The latest resource detail response SHALL include `required_by`: the current-head skills, commands, and subagents of the same Marketplace whose indexed prerequisites include an entry that resolves to this resource under the resource-detail resolution rules, excluding the requested resource itself. Each element SHALL carry the dependent's registry slug, kind, and name, sorted by registry slug, kind, and name, without duplicates. A dependent from another registry SHALL count only when its registry is listed. A dependent SHALL count only when its own resource detail request would succeed for the same caller. The response SHALL omit `required_by` only when an unindexed revision that is visible, in scope, and could match leaves the visible dependent set unknown. Hidden revisions, unlisted revisions from other registries, and revisions of other Marketplaces MUST NOT affect whether `required_by` is present, its contents, or its completeness.

#### Scenario: Same-registry dependent is listed

- **WHEN** a visible current-head resource in the same registry declares an unhinted prerequisite on the requested resource
- **THEN** `required_by` MUST include that dependent

#### Scenario: Listed other-registry dependent is listed

- **WHEN** a visible current-head resource in a listed registry of the same Marketplace declares a prerequisite whose registry hint names the requested resource's registry
- **THEN** `required_by` MUST include that dependent

#### Scenario: Hidden and unlisted dependents are not listed

- **WHEN** a matching dependent would not be returned by its own detail request, or belongs to an unlisted other registry
- **THEN** `required_by` MUST NOT include it

#### Scenario: Visible unindexed candidate makes dependents unknown

- **WHEN** an in-scope, visible current-head revision is unindexed
- **THEN** the response MUST omit `required_by`

#### Scenario: Hidden or unlisted unindexed revisions do not affect completeness

- **WHEN** the only unindexed revisions are hidden from the caller or belong to unlisted other registries
- **THEN** the response MUST include `required_by` computed from the visible in-scope dependents

#### Scenario: Resource without dependents returns an empty list

- **WHEN** every in-scope visible revision is indexed and none requires the requested resource
- **THEN** `required_by` MUST be an empty array
