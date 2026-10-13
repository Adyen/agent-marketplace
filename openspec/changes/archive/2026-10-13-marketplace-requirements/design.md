## Context

`metadata.requires` lets a resource name prerequisites by kind and name, with optional `registry` and `path` hints. The opt-in expansion engine resolves prerequisites from configured Git and directory sources, keeps a separate implied preview when expansion is disabled, and reports typed unmet reasons. Marketplace resources take part only through the satisfaction rule. The Marketplace stores no prerequisites, and a Marketplace resource's own prerequisites are looked up as if its source were Git.

In v4 the Marketplace closure lives in the acquisition lock. Each locked Marketplace resource records `selected_by` edges `{registry_id, name, revision}`. Today every edge must name a locked collection with the same content digest, and every locked resource must be requested or reachable from a requested collection. Configuration holds requests and exact Marketplace exclusions `registry/kind/name`.

## Goals / Non-Goals

**Goals:** index prerequisites at publication; expose same-Marketplace relationships on resource detail; look prerequisites up only in configured Marketplace sources, using the requiring pin's revision semantics; read requires edges; keep removal and lock rewrites correct.

**Non-Goals:** writing requires edges or prerequisite pins; changing the expansion default; CLI, TUI, or web rendering beyond the removal output and the new reason value; search or revision-list fields; legacy v2/v3 configuration formats; Git or directory lookup changes; released-client gate scenarios; performance thresholds.

## Decisions

### Compatibility boundary

The latest release, v4.1.3, decodes acquisition locks strictly. It rejects unknown fields, rejects any `selected_by` edge that does not name a locked collection, and rejects any locked resource that is not reachable. A lock that contains a requires edge is therefore unreadable by v4.1.3, and an implied Marketplace pin cannot be persisted without one. Writing the first requires edge is a lock format change. Release N ships read support and writes none. It never introduces a requires edge, re-points an existing one, or creates a prerequisite pin. Release N+1 starts writing them once N has been released, and the released-client gate moves to N first.

Consequences in release N:
- A lookup hit is never recorded. It appears in the implied preview. When expansion is enabled, the prerequisite is also reported as unmet with the reason `unrecorded`.
- A lock rewrite keeps only requires edges whose parent is still locked with the same digest, and prunes records that only dropped edges held. Updating a requiring resource to a new digest therefore drops its implied pins until a later release refreshes them.
- Removal of a prerequisite held by requires edges deletes lock records and writes an existing exclusion format, so it is compatible.

v4.1.3 decodes resource detail responses leniently, so the new response fields are safe for it. The response schema forbids undeclared properties, so the OpenAPI contract declares the new fields.

### Lock encoding

A `selected_by` entry gains an optional `kind`. Without `kind` it is a collection edge, so existing locks re-encode byte-identically. With a non-collection Marketplace kind, it names the requiring locked resource, and `revision` is that resource's content digest. Lock identity already includes kind, so `(registry_id, kind, name)` identifies the parent uniquely. An explicit `kind: collection` is rejected so that collection edges have a single encoding.

### Publication storage

Prerequisites are a function of content, so they are stored per resource revision as canonical JSON in one nullable column. `NULL` means the revision was published before indexing, and a later re-publication fills it in. There is no backfill. Hints are stored after credential redaction, so the store never holds raw credentials. Matching uses slugs and IDs, which redaction does not alter.

### Resolution and path rules

A registry hint matches a registry of the same Marketplace by slug first, then by ID. A `path` hint names a source root. Stored `source_path` values are file paths relative to the repository, and roots are stored per registry occurrence. The resource's root is therefore derived as the single occurrence root that contains its `source_path`, and compared with the normalized hint for equality. When no single root contains it, the source path is missing, or the hint is unsafe, the target is unresolved rather than guessed.

### Visibility and completeness

An entry is shown only when that resource's own detail request would succeed for the same caller. Dependents in other registries count only when their registry is listed, which matches search. Completeness considers only rows that are visible and in scope. An unindexed dependent removes `required_by` only when it is in scope, visible, and could match. Hidden rows, unlisted rows from other registries, and rows from other tenants never affect presence, contents, or completeness.

### Client lookup

Lookup runs only in online resolution, only for an unmet prerequisite of a Marketplace resource with no matching requires-held pin. It uses only already configured Marketplace entries and their bound registries. With a stored registry commit, and when the target is in the requiring registry, only that exact occurrence and its verified registry revision manifest are consulted, with no fallback to the current revision. The manifest does not carry source paths, so a path-hinted candidate at an exact commit is unresolved. Without a registry commit, or for a hinted different registry, the current revision is used, and its exact occurrence supplies the roots for path matching.

## Risks / Trade-offs

- **Mixed-version teams:** a teammate on N who updates a requiring resource prunes implied pins that N+1 wrote, and the next N+1 install or update restores them. This is accepted for N.
- **Published before indexing:** revisions published before this release show no `requires`, and `required_by` may be omitted until re-publication. Absent and empty stay distinguishable.
- **Nested roots:** ambiguous nested roots leave path hints unresolved. That is conservative, but it reports `not-found` where a narrower rule might resolve.
