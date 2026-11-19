# claude-enterprise-delivery Specification

## Purpose
Define how the Marketplace service mirrors published, allowed resources into a
Claude Enterprise organization, and how operators observe and withdraw delivery.

## Requirements

### Requirement: Claude Enterprise delivery is opt-in and fails closed

The Marketplace service SHALL mirror published resources into a Claude Enterprise
organization only when a `claude_enterprise` configuration block is present and
enabled. While delivery is disabled or absent, the service MUST make no request to
the Claude Plugins API and MUST NOT create delivery state.

The Admin API key MUST be referenced by file or environment variable. An inline
secret value MUST be refused at configuration validation with a coded error, and the
service MUST NOT start with that configuration. The resolved key MUST NOT be written
to the metadata store, logs, metrics, API responses, or CLI output.

Before any upload in a reconcile pass, the service MUST resolve the target
marketplace and verify that it is organization-owned with source `manual`. A
marketplace that is member-owned, synchronized from a repository, or absent MUST
refuse the pass with a stable code and leave existing delivery state unchanged.

#### Scenario: Inline API key is refused at validation

- **GIVEN** a `claude_enterprise` block whose `api_key` carries an inline value
- **WHEN** the service validates its configuration
- **THEN** validation MUST fail with a coded error naming the field
- **AND** the service MUST NOT start and MUST NOT contact the Claude Plugins API

#### Scenario: Disabled delivery makes no outbound request

- **GIVEN** a configuration with no `claude_enterprise` block
- **WHEN** registry publications complete and the worker runs
- **THEN** no Claude Plugins API request MUST be made
- **AND** no delivery state rows MUST be created

#### Scenario: A synchronized marketplace refuses the pass

- **GIVEN** delivery is enabled and the configured marketplace has source `github`
- **WHEN** a reconcile pass runs its preflight
- **THEN** the pass MUST refuse with a stable code identifying the unusable marketplace
- **AND** no plugin, version, or installation setting MUST be written
- **AND** previously recorded delivery state MUST remain unchanged

### Requirement: Only policy-allowed, unrevoked published resources are mirrored

The desired delivery set SHALL be every resource revision of every configured
registry's current publication whose assessment allows it and which carries no
applicable revocation. A rejected or revoked resource revision MUST NOT be built,
MUST NOT be uploaded, and MUST be withdrawn if it was mirrored before.

Collections MUST NOT be mirrored.

#### Scenario: A rejected resource is never uploaded

- **GIVEN** a published resource revision whose assessment rejects it
- **WHEN** a reconcile pass computes its desired set
- **THEN** that revision MUST be absent from the desired set
- **AND** no archive MUST be built or uploaded for it

#### Scenario: A revocation withdraws a mirrored resource

- **GIVEN** a mirrored resource whose revision is then revoked
- **WHEN** the next reconcile pass runs
- **THEN** the plugin's organization-wide installation setting MUST be set to `not_available`
- **AND** the delivery state row MUST record the retired state
- **AND** the plugin MUST NOT be deleted

### Requirement: Each mirrored resource becomes one plugin with a stable derived name

The service SHALL mirror one Claude plugin per resource lineage, identified by its
registry, kind, and resource name. The plugin name SHALL be derived deterministically
from that identity and the configured prefix, contain only lowercase letters, digits,
and hyphens, and be at most 64 characters. A derivation that would exceed the limit
MUST be truncated and suffixed with a digest of the full identity so two distinct
identities cannot produce one name.

The derived name MUST be recorded when the plugin is first published and MUST be
reused unchanged for the lifetime of that plugin, including after the registry slug
or configured prefix changes.

#### Scenario: A long identity produces a bounded unique name

- **GIVEN** a resource identity whose derived name would exceed 64 characters
- **WHEN** the plugin name is derived
- **THEN** the name MUST be at most 64 characters and match the allowed character set
- **AND** two identities differing only beyond the truncation point MUST produce different names

#### Scenario: A renamed registry slug does not rename a live plugin

- **GIVEN** a mirrored resource whose registry slug changes in server configuration
- **WHEN** the next reconcile pass runs
- **THEN** the existing plugin MUST keep its recorded name
- **AND** no second plugin MUST be created for the same resource identity

### Requirement: Archives are built deterministically in the Claude plugin layout

The service SHALL build each plugin archive from the stored canonical artifact of the
published resource revision, placing its content by kind: a skill at
`skills/<name>/`, a command at `commands/<name>.md`, a subagent at
`agents/<name>.md`, a hook in `hooks/hooks.json` with its support files beneath
`hooks/<name>/`, and an MCP server in `.mcp.json`. Exactly one manifest MUST be
generated at `.claude-plugin/plugin.json`, carrying the derived plugin name, the
resource display name bounded to 64 characters, and the resource description bounded
to 500 characters.

Building the same resource revision twice MUST produce byte-identical archives.

#### Scenario: Repeated builds are byte-identical

- **GIVEN** a published resource revision
- **WHEN** its plugin archive is built twice in separate processes
- **THEN** both archives MUST be byte-identical
- **AND** their archive digests MUST be equal

#### Scenario: The mirrored resource keeps its own name

- **GIVEN** a skill named `threat-model` mirrored as a plugin with a derived name
- **WHEN** the archive is built
- **THEN** the skill MUST be placed at `skills/threat-model/SKILL.md`
- **AND** the manifest `name` MUST be the derived plugin name

### Requirement: Archives are validated locally before any upload

The service SHALL validate every built archive against the documented upload
requirements before sending a request, including exactly one manifest, the manifest
name character set and length, bounded display name and description, valid
`SKILL.md` frontmatter carrying `name` and `description` without XML tags, no
duplicate skill or command names, no top-level `bin/` directory, no nested archive,
relative paths within the permitted character set and depth, the file-count and
uncompressed-size ceilings, and archive entries that are neither encrypted nor
symbolic links.

A resource whose archive fails validation MUST be skipped with a stable diagnostic
code, MUST NOT be uploaded, and MUST NOT fail the reconcile pass for other resources.

#### Scenario: An invalid archive never reaches the API

- **GIVEN** a published resource whose built archive contains a top-level `bin/` directory
- **WHEN** a reconcile pass processes it
- **THEN** no upload request MUST be made for that resource
- **AND** its delivery state MUST record the skipped state with a stable code
- **AND** other resources in the same pass MUST still be processed

### Requirement: Unchanged content is never re-uploaded

The service SHALL record the archive digest of each successful upload and SHALL
upload a resource again only when its newly built archive digest differs from the
recorded one. A reconcile pass over unchanged publications MUST perform no write
request.

#### Scenario: A repeated pass over unchanged content writes nothing

- **GIVEN** every desired resource has been mirrored successfully
- **WHEN** a further reconcile pass runs without any publication or revocation change
- **THEN** no plugin, version, or installation-setting write MUST be sent
- **AND** every delivery state row MUST keep its recorded version identifiers

#### Scenario: A changed resource uploads exactly one version

- **GIVEN** a mirrored resource whose content changes in a new publication
- **WHEN** a reconcile pass runs
- **THEN** exactly one version create MUST be sent for that plugin
- **AND** the recorded archive digest and published version identifier MUST be updated

### Requirement: Plugins are adopted only with proof of ownership

Before creating a plugin, the service SHALL read the marketplace inventory. An
existing plugin holding the derived name SHALL be adopted only when its creator is an
API actor carrying the configured key's identifier. Any other existing plugin,
including one with no recorded creator, MUST be left untouched and recorded as a
conflict with a stable code, and the service MUST NOT upload a version into it.

#### Scenario: A foreign plugin with the same name is never written to

- **GIVEN** a plugin in the target marketplace holding the derived name, created by an administrator in claude.ai
- **WHEN** a reconcile pass would publish that resource
- **THEN** no version MUST be uploaded into that plugin
- **AND** the delivery state MUST record a conflict with a stable code

#### Scenario: A plugin created by this key is adopted

- **GIVEN** a plugin holding the derived name whose creator is an API actor with the configured key identifier
- **AND** no delivery state row exists for it
- **WHEN** a reconcile pass runs
- **THEN** the plugin MUST be adopted into delivery state
- **AND** a duplicate plugin MUST NOT be created

### Requirement: Availability is set per plugin and never on the marketplace

After creating a plugin, the service SHALL set that plugin's organization-wide
installation setting to the configured preference, which defaults to `available`.
The service MUST NOT write any marketplace default installation preference, and MUST
NOT change the installation setting of a plugin it has not adopted.

Installation-setting writes for one plugin MUST be sent one at a time.

#### Scenario: A newly created plugin becomes available to members

- **GIVEN** delivery is configured with the default installation preference
- **WHEN** a resource is mirrored for the first time
- **THEN** an organization-wide installation setting of `available` MUST be set for that plugin
- **AND** no marketplace default MUST be written

### Requirement: Withdrawal is availability-only and reversible

A mirrored resource that leaves the desired set SHALL be withdrawn by setting its
plugin's organization-wide installation setting to `not_available` and recording the
retired state. The service MUST NOT delete plugins or versions.

A withdrawn resource that returns to the desired set SHALL be republished into its
existing plugin, with its installation setting restored to the configured preference.

Operator-facing status for a withdrawn plugin MUST state that previously installed
copies and stored version history are unaffected.

#### Scenario: A returning resource reuses its plugin

- **GIVEN** a retired delivery state row for a resource that is published again
- **WHEN** a reconcile pass runs
- **THEN** the existing plugin MUST receive the new version
- **AND** its installation setting MUST be restored to the configured preference
- **AND** no second plugin MUST be created

### Requirement: The served version follows the latest upload

The service SHALL leave mirrored plugins unpinned and MUST NOT change a plugin's
served version. It SHALL record the reported content-scan status and reach of each
published version, and SHALL report a plugin whose served version is withheld by a
content scan.

#### Scenario: A withheld version is visible to the operator

- **GIVEN** an uploaded version whose content scan is reported as processing
- **WHEN** the operator reads delivery status
- **THEN** the plugin MUST be reported as awaiting its content scan
- **AND** no served-version change MUST have been sent

### Requirement: Uploads follow the documented retry rules

The service SHALL treat a `registration_pending` failure of a plugin create as a
created plugin, and SHALL complete it by uploading the same files as a version of the
identified plugin rather than resending the create. It SHALL treat
`registration_pending` on a version create as a stored version and resend only when
the response permits a retry. Before each version upload it SHALL record the plugin's
latest version identifier, and after a lost response it SHALL re-read the plugin and
retry only when that identifier is unchanged.

A rate-limited response MUST be honoured by waiting for the interval the response
names, and the remaining work MUST resume in a later pass without loss.

Unrecognized component types, reach values, scan statuses, marketplace sources, and
error codes MUST be treated as opaque values rather than failures.

#### Scenario: A pending registration is completed without a duplicate plugin

- **GIVEN** a plugin create that fails with a pending registration naming the created plugin
- **WHEN** the reconciler continues
- **THEN** it MUST NOT resend the create
- **AND** it MUST upload the same files as a version of the named plugin

#### Scenario: A lost version response does not store a duplicate version

- **GIVEN** a version upload whose response is lost after the latest version identifier was recorded
- **WHEN** the reconciler retries
- **THEN** it MUST re-read the plugin first
- **AND** it MUST retry only when the latest version identifier is unchanged

### Requirement: Reconciliation is leased, paced, and resumable

Delivery reconciliation SHALL run as a leased worker job, with at most one pass in
flight for the instance. A pass SHALL be enqueued after a registry publication
completes, when revocations change, on the configured repair interval, and on
operator demand.

A pass that is interrupted, refused, or rate-limited MUST leave recorded per-plugin
progress intact so the next pass resumes rather than restarts, and MUST record a
sanitized failure cause.

#### Scenario: An interrupted pass resumes rather than restarts

- **GIVEN** a reconcile pass that uploaded some plugins before being interrupted
- **WHEN** a later pass runs
- **THEN** the already-uploaded plugins MUST NOT be uploaded again
- **AND** the remaining resources MUST be processed

#### Scenario: Publication completion enqueues delivery

- **GIVEN** delivery is enabled
- **WHEN** a registry publication completes
- **THEN** a delivery job MUST be enqueued for that instance
- **AND** a second concurrent pass MUST NOT run

### Requirement: Delivery state is observable without exposing content

The service SHALL expose per-plugin delivery state to operators through the CLI and a
read-only admin endpoint, reporting the resource identity, plugin name and
identifier, state, published and served version identifiers, content-scan status,
reach, installation preference, and the stable code of the last failure.

Delivery output, logs, and metrics MUST carry identity, codes, and counts only, and
MUST NOT carry the Admin API key, resource bodies, hook commands, MCP server
environment values, or request bodies.

#### Scenario: Status reports a failure by code without content

- **GIVEN** a resource whose upload failed
- **WHEN** the operator reads delivery status
- **THEN** the output MUST name the resource, the plugin, and a stable failure code
- **AND** the output MUST NOT contain the Admin API key or any resource body
