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

**Owned entry**:
An element inside a shared harness configuration file that Agent Marketplace is demonstrably authorized to reconcile and prune.
_Avoid_: Generated file, managed file

**Ownership state**:
Evidence identifying owned entries in shared harness configuration files.
_Avoid_: Cache, ownership marker

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
