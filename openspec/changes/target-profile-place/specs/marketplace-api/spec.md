## MODIFIED Requirements

### Requirement: Capabilities document and wire vocabulary

A Marketplace SHALL serve a capabilities document declaring its API major
versions, artifact schema versions, selectable resource kinds, acquisition
modes, and optional server features. The server, CLI, and generated clients
SHALL share one wire vocabulary of selectable resource kinds.

The `target-profile-place-v1` feature SHALL extend that vocabulary with the
virtual `target-profile` kind and scoped package kinds whose target and local
kind are declared in the exact assessed profile. Requests opt in with
`X-Agent-Marketplace-Features: target-profile-place-v1`. The base
`resource_kinds` enum SHALL remain unchanged. Without the opt-in, legacy
discovery, search and selection MUST retain the base vocabulary: filter
place-only records before pagination and refuse selection of those records
instead of returning a manifest with unsupported member kinds. Cacheable
responses that vary by opt-in MUST distinguish the two representations.

#### Scenario: Capabilities document matches the OpenAPI contract

- **WHEN** the capabilities document of a Marketplace instance is produced
- **THEN** it MUST declare the API major versions `["v1"]`
- **AND** the artifact schema versions `["v1"]`
- **AND** its resource kinds MUST match the `resource_kinds` enum declared in
  the canonical OpenAPI document

#### Scenario: Capabilities document declares acquisition modes and server features

- **WHEN** the capabilities document of a Marketplace instance is produced
- **THEN** it MUST declare the index-only and mirror acquisition modes
- **AND** it MUST advertise the catalog projection contract
  (`catalog-projection-v1alpha1`) as a server feature

#### Scenario: Selectable resource kinds

- **WHEN** the base selectable resource kind vocabulary is inspected
- **THEN** it MUST contain `skill`, `command`, `subagent`, `hook`, `mcpserver`,
  `collection` in that order
- **AND** exactly those values MUST be selectable without the place feature;
  non-selectable kinds such as `rule` MUST be rejected

#### Scenario: Bidirectional kind conversion

- **WHEN** Marketplace wire kind values are converted to and from the
  harness-neutral resource representation
- **THEN** the `skill`, `command`, `subagent`, `hook`, and `mcpserver` kinds
  MUST map in both directions
- **AND** conversion into the harness-neutral representation MUST reject
  virtual kinds, scoped place kinds and unknown values

#### Scenario: Place discovery does not change legacy discovery

- **WHEN** an instance advertises `target-profile-place-v1` and serves both
  legacy and place resources
- **THEN** a legacy request MUST see only the base kinds and compatible
  selections, with totals and pages computed from that set
- **AND** an opted-in request MUST also be able to discover and select the
  virtual profile and packages assessed against it

## ADDED Requirements

### Requirement: Place package wire identity

Scoped package kind values SHALL use `<target-id>/<local-kind>` in structured
data. In a Marketplace API or browser route the kind-internal slash SHALL be
the single literal escape `%2F`; split the escaped route before decoding and
decode that token once. Other tokens and existing kind routes retain their
existing syntax. An invalid, noncanonical, extra or ambiguous escape in the
scoped kind MUST refuse before lookup.

#### Scenario: Scoped package routes use a single kind escape

- **WHEN** a profile-aware client requests a declared scoped kind using its
  canonical escaped route
- **THEN** the API MUST resolve the exact structured kind
- **AND** lowercase, double-escaped and raw slash alternatives MUST refuse,
  without changing matching for existing routes

### Requirement: Place artifact binding

The schema-v1 place artifact SHALL carry the exact payload and file map.
The manifest and assessment SHALL agree on `host_package_identity`: the
payload digest, mapping digest, full profile digest and exact profile
identity. The canonical assessment digest MUST cover the binding; the
place content digest is the payload tree digest. Compiled kinds retain
their existing digest and encoding.

#### Scenario: Place manifests and assessments agree on the exact binding

- **WHEN** a scoped place package is exposed through the Marketplace
- **THEN** its manifest and assessment MUST agree on the profile, payload
  and mapping identities and verify against their canonical digests
- **AND** an absent or mismatched binding MUST fail verification rather
  than fall back to ordinary artifact acceptance
