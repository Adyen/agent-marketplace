## MODIFIED Requirements

### Requirement: Resource detail exposes prerequisites

The latest resource detail response SHALL include `requires` exactly when the revision is indexed: an empty array when it declares none, and omitted when it is unindexed. Each entry SHALL carry the declared kind and name and the stored, credential-redacted `registry` and `path` hints. An entry SHALL include `resolved` with the target's registry slug, kind, and name only when all of the following hold. The target registry is the requiring registry when there is no registry hint, or otherwise the registry of the same Marketplace whose slug, or failing that whose ID, equals the hint. The target has a current-head occurrence. When a `path` hint is present, the target's source root equals the normalized hint. The target's own resource detail request would succeed for the same caller. The source root is the single root of the target's registry occurrence that contains the target's source path. When no single root qualifies, the source path is unknown, or the hint is absolute, escaping, or otherwise unsafe, the path hint is unmatched. A hidden target MUST be indistinguishable from a missing one. In addition, an opted-in exact revision request for an available or operator-exposed, non-revoked Place package SHALL return only that revision's stored prerequisite kind and name declarations when indexed. Exact Place declarations MUST NOT be resolved against current heads or include registry, path, `resolved`, or `required_by` fields. An absent or `null` indexed value SHALL remain omitted. Search, revision-list, and ordinary revision-by-digest responses MUST NOT gain these fields.

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

#### Scenario: Exact Place revision returns its stored declarations

- **WHEN** a caller opts in to Place and requests an available, non-revoked scoped package revision by its exact digest
- **THEN** `requires` MUST contain that revision's indexed kind and name declarations in declaration order, without hints, `resolved`, or `required_by`

#### Scenario: Exact historical Place revision does not use the current head

- **WHEN** an opted-in caller requests an older, available Place package revision after a newer revision with different prerequisites has become current
- **THEN** `requires` MUST contain only the older exact revision's stored declarations
- **AND** it MUST NOT contain `resolved` or `required_by`

#### Scenario: Exact Place indexed-empty and unknown prerequisites stay distinct

- **WHEN** an opted-in caller requests an available exact Place revision whose stored value is `[]`, absent, or `null`
- **THEN** `requires` MUST be `[]` only for the explicitly indexed empty array, and MUST be omitted for absent or `null`

#### Scenario: Ineligible exact Place revision does not expose prerequisites

- **WHEN** an exact Place revision is revoked or not available to the caller
- **THEN** the response MUST NOT expose its `requires`

#### Scenario: Search and revision responses are unchanged

- **WHEN** a caller searches resources, lists revisions, or requests an ordinary resource revision by digest, rather than an opted-in exact Place package revision
- **THEN** those responses MUST NOT include `requires` or `required_by`

#### Scenario: Invalid exact Place declarations are refused

- **WHEN** an exact Place revision contains malformed declarations, registry or path hints, a foreign target, or a self-reference
- **THEN** the API MUST refuse the response instead of exposing a successful prerequisite graph
