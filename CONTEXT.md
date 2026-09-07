# Agent Marketplace

Agent Marketplace distributes harness-neutral agent resources and materializes them into layouts understood by agent runtimes.

## Language

**Harness target**:
A destination runtime format into which harness-neutral resources are compiled.
_Avoid_: Provider, compatibility layout

**Source layout**:
The canonical on-disk format from which a resource bundle is ingested.
_Avoid_: Harness target, import path

**Source marker**:
A harness-specific path whose presence is sufficient to autodetect a source layout. A source marker identifies the layout but does not contain every resource in it.
_Avoid_: Install root, source layout

**Full harness support**:
A harness target and source layout that supports compilation, ingestion, bidirectional conversion, UI selection, repository and machine scopes, and every marketplace-selectable resource kind. Unsupported non-selectable IR kinds are diagnosed explicitly.
_Avoid_: Registry entry, partial compiler

**Converted command skill**:
A command represented as a skill in a harness that has no command primitive while retaining its command identity.
_Avoid_: Native command

**Owned entry**:
An element inside a shared harness configuration file that Agent Marketplace is demonstrably authorized to reconcile and prune.
_Avoid_: Generated file, managed file

**Ownership state**:
Evidence identifying owned entries in shared harness configuration files.
_Avoid_: Cache, ownership marker

**Resource**:
A first-class agent capability distributed by Agent Marketplace. Materialized resource kinds include skill, command, subagent, hook, and MCP server; collection is a virtual resource kind.
_Avoid_: Skill, when referring to resources of every kind

**Marketplace**:
A browsable service through which Agent Marketplace users discover and acquire resources indexed from approved registries.
_Avoid_: Registry, registry service

**Marketplace endpoint**:
The canonical HTTP API location of one Marketplace, identified by a stable Marketplace instance ID.
_Avoid_: Registry origin, Marketplace alias

**Marketplace endpoint registration**:
A consumer configuration record that makes a Marketplace endpoint available for discovery without selecting any resource.
_Avoid_: Marketplace installation, resource pin

**Registry repository**:
A Git repository that remains the publishing source of truth for resources indexed by a Marketplace.
_Avoid_: Marketplace, artifact store

**Registry root**:
A configured directory within a registry repository from which a Marketplace recursively discovers source-layout bundles, stopping descent at each bundle. The root itself need not be a bundle.
_Avoid_: Skill directory, bundle root

**Marketplace submission**:
A request to add a registry to a Marketplace. A submission is not discoverable until approved through that Marketplace's vetting process.
_Avoid_: Published registry, automatic crawl

**Index-only acquisition**:
An acquisition mode in which a Marketplace resolves a resource to a pinned registry occurrence and the consumer acquires and verifies its content from Git.
_Avoid_: Artifact mirror

**Mirror acquisition**:
An acquisition mode in which a Marketplace distributes an immutable resource artifact indexed from a pinned registry occurrence.
_Avoid_: Publishing registry, mutable package host

**Policy domain**:
The shared administration, identity, registry approval, and trust boundary served by one Marketplace deployment.
_Avoid_: Tenant

**Resource coordinate**:
The stable Marketplace identity of a resource, composed from its registry identity, resource kind, and registry-local name.
_Avoid_: Installed name, harness path

**Resource revision**:
An immutable canonical content state of a resource coordinate, identified by its content digest. Its artifact contains only intrinsic resource data, and the same resource revision may occur in more than one registry commit or receive more than one resource assessment.
_Avoid_: Resource assessment, registry occurrence, branch, mutable tag

**Resource assessment**:
An immutable, content-addressed analyzer and operator-policy decision about one exact resource revision. Assessments retain findings and an inspection projection reference; one assessment is selected as current without changing the resource revision.
_Avoid_: Resource revision, revocation, safety certification

**Registry occurrence**:
One exact registry repository commit, the registry roots used to ingest it, and its analyzer-independent registry revision manifest.
_Avoid_: Resource occurrence, resource revision, mutable ref

**Resource occurrence**:
The membership of one exact resource revision in one registry occurrence.
_Avoid_: Registry occurrence, resource revision, current assessment

**Installed name**:
The local, user-facing name under which a resource is materialized into a harness. It may be shorter than, and must not be confused with, its resource coordinate.
_Avoid_: Resource coordinate

**Marketplace alias**:
A local shorthand for a Marketplace endpoint used in discovery and installation commands.
_Avoid_: Registry identity, installed name

**Registry ID**:
The immutable Marketplace identity assigned to a registry. It survives repository moves and is stored in installation records.
_Avoid_: Registry slug, registry origin

**Registry slug**:
The human-readable Marketplace locator for a registry. Its canonical value may change, but prior values remain permanent, non-reassignable aliases.
_Avoid_: Registry ID, repository URL

**Registry origin**:
The current Git location from which a Marketplace indexes a registry. Its recorded history provides provenance across repository moves.
_Avoid_: Registry ID, Marketplace endpoint

**Registry revision manifest**:
A record connecting every resource occurrence indexed from one registry repository commit to its resource revision, optional mirrored artifact, and shared provenance.
_Avoid_: Resource artifact

**Resource artifact**:
An immutable, content-addressed canonical representation of one resource revision and its contained supporting files.
_Avoid_: Registry repository, registry revision manifest

**Collection revision**:
An immutable set of exact resource revisions selected by one collection at publication time. A collection revision is virtual and never materializes directly.
_Avoid_: Resource artifact, mutable collection selection

**Collection manifest**:
The content-addressed canonical representation of one collection revision, preserving exact direct collection includes and a deduplicated expansion of exact, non-collection member revisions.
_Avoid_: Resource artifact, collection installation

**Effective revocation**:
The revocation outcome for a resource revision when any active revision-, coordinate-, or registry-level operator declaration matches it. Specificity selects the primary reason but cannot exempt it from a broader declaration.
_Avoid_: Deleted resource, stored revision metadata

**Inspection projection**:
A bounded, immutable, analyzer-independent representation of a resource revision that exposes every textual file, contained raster previews, and binary-file identity before installation. Resource assessment findings reference its canonical paths and byte ranges.
_Avoid_: Resource assessment, resource artifact, live preview, safety certification

**Marketplace installation**:
A consumer configuration record that pins a resource coordinate to an exact Marketplace instance, resource revision, canonical content digest, acquisition mode, optional mirrored artifact digest, exact registry occurrence for index-only acquisition, and optional installed name.
_Avoid_: Search result, Marketplace alias

**Installation selector**:
A reason a resource pin remains installed: either direct user selection or selection by an exact collection revision. A pin is removable only when it has no remaining selectors.
_Avoid_: Resource dependency, duplicate pin

**Security finding**:
A versioned analyzer observation retained by a resource assessment about potentially dangerous or hidden resource content. A finding communicates evidence; the assessment carries the operator-policy decision and is not a claim that a resource is safe or malicious.
_Avoid_: Safety certification

**Revoked revision**:
A resource revision that a Marketplace retains for provenance but refuses to resolve for ordinary installation, update, and online synchronization. Revocation cannot recall bytes already held by clients or intermediary caches.
_Avoid_: Deleted resource

**Marketplace operator**:
The party that administers a Marketplace and decides which registries and policies it applies.
_Avoid_: Registry publisher

**Marketplace operator configuration**:
Deployment-owned configuration defining a Marketplace instance, its storage, indexing policy, approved registries, refresh behavior, and revocations.
_Avoid_: Agent Marketplace configuration, organization configuration, consumer configuration

**Consumer organization configuration**:
Externally managed machine configuration that constrains Agent Marketplace client behavior for an organization.
_Avoid_: Marketplace operator configuration, Marketplace configuration

**Registry publisher**:
The party represented as publishing a registry repository. Publisher identity is distinct from Marketplace admission and operator endorsement.
_Avoid_: Marketplace operator
**Community telemetry consent**:
An affirmative, user-controlled choice made during interactive initialization before an unmanaged installation sends community telemetry.
_Avoid_: Opt-out telemetry, implied consent

**Organization telemetry policy**:
Telemetry requirements explicitly declared by the externally managed organization configuration and inspectable, but not overridable, by the user.
_Avoid_: Inferred organization policy, user preference

**Primary telemetry route**:
The route that receives telemetry at the effective level: the organization endpoint for a managed installation, otherwise a configured user endpoint or the project endpoint.
_Avoid_: Community report

**Community report**:
The basic telemetry schema sent to the project endpoint, authorized by the user on an unmanaged installation or by organization policy on a managed installation.
_Avoid_: Primary telemetry route, detailed telemetry

**Telemetry level**:
The persistent disclosure depth selected from off, basic, or detailed.
_Avoid_: Diagnostic session, OpenTelemetry signal

**Basic telemetry**:
Low-cardinality adoption and invocation-health information.
_Avoid_: Anonymous telemetry

**Detailed telemetry**:
Lifecycle metrics and traces that can identify marketplace resources and correlate stages of an operation.
_Avoid_: Diagnostic telemetry, unrestricted capture

**Diagnostic session**:
A deliberate, temporary support session that lets the user review and export evidence for one investigated problem.
_Avoid_: Debug logging, unrestricted capture

**Installation ID**:
A random, visible, resettable identifier for one Agent Marketplace installation.
_Avoid_: User identity, device fingerprint

**Diagnostic reference**:
A value shown to the user that identifies telemetry for a specific diagnostic operation.
_Avoid_: Installation ID

**Pending telemetry record**:
A normal basic or detailed telemetry record awaiting bounded best-effort delivery.
_Avoid_: Diagnostic payload, operational log
