# Agent Marketplace

Agent Marketplace distributes harness-neutral agent resources and materializes them into layouts understood by agent runtimes.

## Language

**Harness target**:
A destination runtime format into which harness-neutral resources are compiled.
_Avoid_: Provider, compatibility layout

**Materialization mode**:
The choice to install generated resource files and directory trees as regular copies or cache-backed symlinks, independent of the destination runtime format and ownership of entries in shared configuration.
_Avoid_: Harness capability, compiler format

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

**Acquisition lock**:
A generated, layer-owned record that binds a remote Git requirement to an exact commit and verified source revision manifest without changing selection precedence.
_Avoid_: Effective configuration, Marketplace lock

**Exact snapshot**:
An immutable Git cache entry opened by an algorithm-qualified commit and verified against the acquisition lock's source manifest.
_Avoid_: Mutable ref cache, working tree

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
A browsable service through which Agent Marketplace users discover and acquire resources indexed from approved source repositories.
_Avoid_: Registry, registry service

**Marketplace endpoint**:
The canonical HTTP API location of one Marketplace, identified by a stable Marketplace instance ID.
_Avoid_: Source origin, Marketplace alias

**Source repository**:
A Git repository that remains the publishing source of truth for resources indexed by a Marketplace.
_Avoid_: Marketplace, artifact store

**Marketplace submission**:
A request to add a source repository to a Marketplace. A submission is not discoverable until approved through that Marketplace's vetting process.
_Avoid_: Published source, automatic crawl

**Index-only acquisition**:
An acquisition mode in which a Marketplace resolves a resource to a pinned source occurrence and the consumer acquires and verifies its content from Git.
_Avoid_: Artifact mirror

**Mirror acquisition**:
An acquisition mode in which a Marketplace distributes an immutable resource artifact indexed from a pinned source occurrence.
_Avoid_: Publishing source, mutable package host

**Policy domain**:
The shared administration, identity, source-approval, and trust boundary served by one Marketplace deployment.
_Avoid_: Tenant

**Resource coordinate**:
The stable Marketplace identity of a resource, composed from its source identity, resource kind, and source-local name.
_Avoid_: Installed name, harness path

**Resource revision**:
An immutable canonical content and metadata state of a resource coordinate, identified by its content digest. The same resource revision may occur in more than one source commit.
_Avoid_: Source occurrence, branch, mutable tag

**Source occurrence**:
The appearance of a resource revision in one exact source repository commit.
_Avoid_: Resource revision, mutable ref

**Installed name**:
The local, user-facing name under which a resource is materialized into a harness. It may be shorter than, and must not be confused with, its resource coordinate.
_Avoid_: Resource coordinate

**Marketplace alias**:
A local shorthand for a Marketplace endpoint used in discovery and installation commands.
_Avoid_: Source identity, installed name

**Source ID**:
The immutable Marketplace identity assigned to an approved source. It survives repository moves and is stored in installation records.
_Avoid_: Source slug, source origin

**Source slug**:
The human-readable Marketplace locator for an approved source. Its canonical value may change, but prior values remain permanent, non-reassignable aliases.
_Avoid_: Source ID, repository URL

**Source origin**:
The current Git location from which a Marketplace indexes an approved source. Its recorded history provides provenance across repository moves.
_Avoid_: Source ID, Marketplace endpoint

**Source revision manifest**:
A record connecting every resource occurrence indexed from one source repository commit to its resource revision, optional mirrored artifact, and shared provenance.
_Avoid_: Resource artifact

**Resource artifact**:
An immutable, content-addressed canonical representation of one resource revision and its contained supporting files.
_Avoid_: Source repository, source revision manifest

**Collection revision**:
An immutable set of exact resource revisions selected by one collection at publication time. A collection revision is virtual and never materializes directly.
_Avoid_: Resource artifact, mutable collection selection

**Collection manifest**:
The content-addressed canonical representation of one collection revision, preserving exact direct collection includes and a deduplicated expansion of exact, non-collection member revisions.
_Avoid_: Resource artifact, collection installation

**Effective revocation**:
The revocation outcome for a resource revision when any active revision-, coordinate-, or source-level operator declaration matches it. Specificity selects the primary reason but cannot exempt it from a broader declaration.
_Avoid_: Deleted resource, stored revision metadata

**Inspection projection**:
A bounded, immutable representation of a resource revision that exposes every textual file, contained raster previews, binary-file identity, and security findings before installation.
_Avoid_: Resource artifact, live preview, safety certification

**Marketplace installation**:
A consumer configuration record that pins a resource coordinate to an exact Marketplace instance, resource revision, canonical content digest, acquisition mode, optional mirrored artifact digest, exact source occurrence for index-only acquisition, and optional installed name.
_Avoid_: Search result, Marketplace alias

**Installation selector**:
A reason a resource pin remains installed: either direct user selection or selection by an exact collection revision. A pin is removable only when it has no remaining selectors.
_Avoid_: Resource dependency, duplicate pin

**Security finding**:
A versioned analyzer observation about potentially dangerous or hidden resource content. A finding communicates evidence and policy outcome; it is not a claim that a resource is safe or malicious.
_Avoid_: Safety certification

**Revoked revision**:
A resource revision that a Marketplace retains for provenance but refuses to resolve for ordinary installation, update, and online synchronization. Revocation cannot recall bytes already held by clients or intermediary caches.
_Avoid_: Deleted resource

**Marketplace operator**:
The party that administers a Marketplace and decides which sources and policies it applies.
_Avoid_: Source publisher

**Source publisher**:
The party represented as publishing a source repository. Publisher identity is distinct from Marketplace admission and operator endorsement.
_Avoid_: Marketplace operator
