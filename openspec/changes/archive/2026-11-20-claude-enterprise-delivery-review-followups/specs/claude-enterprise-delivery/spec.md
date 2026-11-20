## ADDED Requirements

### Requirement: Archive validation and serialization preserve the Claude input contract

The service MUST reject archive file-count and uncompressed-size violations from
ZIP metadata before reading entry bodies. An insecure ZIP path MUST report
`archive.path-invalid` regardless of ZIP reader strictness. Plain comparison text
in skill metadata MUST NOT be rejected as an XML tag.

A remote MCP server that supplies a native `type` selector without a canonical
transport MUST keep that selector in the generated `.mcp.json`. When a canonical
transport is present, it MUST take precedence.

#### Scenario: Archive limits are checked before entry bodies

- **GIVEN** a ZIP with an excessive declared file count or uncompressed size
- **WHEN** the service validates it
- **THEN** it MUST return the applicable limit diagnostic without reading entry bodies

#### Scenario: Strict ZIP mode keeps the path diagnostic

- **GIVEN** a ZIP with an entry whose path contains a backslash
- **WHEN** the service validates it with strict ZIP path checking
- **THEN** it MUST report `archive.path-invalid`

#### Scenario: A comparison is not an XML tag

- **GIVEN** a valid skill with the description `Compare x < y and z > w`
- **WHEN** the delivery archive is validated
- **THEN** the description MUST NOT cause `skill.frontmatter-xml`

#### Scenario: A raw remote MCP selector survives mirroring

- **GIVEN** a remote MCP server with a URL and `type: http` but no canonical transport
- **WHEN** the service builds the plugin archive
- **THEN** the server's `.mcp.json` entry MUST keep `type: http`

### Requirement: Failed delivery does not reserve an unused plugin name

An existing plugin claim MUST remain associated with its recorded resource.
An invalid or unavailable archive MUST NOT claim a derived name that no resource
has yet published.

#### Scenario: An unavailable archive does not block a valid sibling

- **GIVEN** two desired resources with the same unused derived name and an unavailable archive for the first
- **WHEN** the reconcile pass processes them
- **THEN** it MUST record the first resource's failure and publish the valid sibling

## MODIFIED Requirements

### Requirement: Claude Enterprise delivery is opt-in and fails closed

The Marketplace service SHALL mirror published resources into a Claude Enterprise
organization only when a `claude_enterprise` configuration block is present and
enabled. While delivery is disabled or absent, the service MUST make no request to
the Claude Plugins API and MUST NOT create delivery state.

The Admin API key MUST be referenced by file or environment variable. An inline
secret value MUST be refused with a coded error before decoding its value, and the
service MUST NOT start with that configuration. An inline or resolved key MUST NOT
be written to the metadata store, logs, metrics, API responses, or CLI output.

Before any upload in a reconcile pass, the service MUST resolve the target
marketplace and verify that it is organization-owned with source `manual`. A
marketplace that is member-owned, synchronized from a repository, or absent MUST
refuse the pass with a stable code and leave existing delivery state unchanged.

#### Scenario: Inline API key is refused at validation

- **GIVEN** a `claude_enterprise` block whose `api_key` carries an inline value, even one with a malformed scalar type
- **WHEN** the service reads its configuration
- **THEN** it MUST fail with a coded error naming the field without rendering the value
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

Collections MUST NOT be mirrored. A withdrawn or retired managed registry MUST
be excluded from the desired set. Pausing a registry without withdrawing it MUST
NOT by itself exclude its current publication.

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

#### Scenario: Pause does not withdraw a registry publication

- **GIVEN** a managed registry is paused and then withdrawn
- **WHEN** delivery computes registry eligibility after each action
- **THEN** pause alone MUST keep the registry eligible
- **AND** withdrawal MUST exclude the registry

### Requirement: Withdrawal is availability-only and reversible

A mirrored resource that leaves the desired set SHALL be withdrawn by setting its
plugin's organization-wide installation setting to `not_available` and recording the
retired state. The service MUST NOT delete plugins or versions or change group
installation settings.

A withdrawn resource that returns to the desired set SHALL reuse its existing
plugin, with its installation setting restored to the configured preference. It
SHALL upload a new version only if the archive digest changed.

Operator-facing status for a withdrawn plugin MUST state that previously installed
copies and stored version history are unaffected.

#### Scenario: A returning resource reuses its plugin

- **GIVEN** a retired delivery state row for a resource that is published again
- **WHEN** a reconcile pass runs
- **THEN** the existing plugin MUST receive a new version only if its archive digest changed
- **AND** its installation setting MUST be restored to the configured preference
- **AND** no second plugin MUST be created

### Requirement: The served version follows the latest upload

The service MUST NOT pin a mirrored plugin or change its served version. It SHALL
record the reported content-scan status and reach of the uploaded version even
when Claude serves an older version. It SHALL report when the uploaded version
is awaiting a scan or when a different version remains served because of a pin.
The scan message takes precedence while the uploaded version is processing.

#### Scenario: A withheld version is visible to the operator

- **GIVEN** an uploaded version whose content scan is reported as processing
- **WHEN** the operator reads delivery status
- **THEN** the plugin MUST be reported as awaiting its content scan
- **AND** no served-version change MUST have been sent

#### Scenario: An operator pin remains in place

- **GIVEN** an uploaded version and a different pinned served version
- **WHEN** a repeated pass refreshes delivery status and the uploaded version completes its scan
- **THEN** status MUST use the uploaded version's scan and report that a different version remains served
- **AND** no version upload or served-version change MUST be sent

### Requirement: Uploads follow the documented retry rules

The service SHALL treat a `registration_pending` failure of a plugin create as a
created plugin, and SHALL complete it by uploading the same files as a version of the
identified plugin rather than resending the create. It SHALL treat
`registration_pending` on a version create as a stored version and resend only when
the response permits a retry within the same pass. Before each version upload it
SHALL record the plugin's latest version identifier, and after a lost response it
SHALL re-read the plugin and retry only when that identifier is unchanged.

When a later read confirms a pending archive is already served, delivery SHALL
recover without a new upload. A served version with a different identifier from the
pending one MUST also be the latest version, carry the archive marker, and name the
configured API-key actor.

A rate-limited response MUST be honoured by waiting for the interval the response
names, and the remaining work MUST resume in a later pass without loss. A
rate-limited write MUST return to the worker rather than bypass write pacing with
an inline retry.

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

#### Scenario: An already-served registration retry is recovered without writing

- **GIVEN** an authorized registration retry whose write response and follow-up read are lost
- **WHEN** a later pass finds a new served and latest version
- **THEN** it MUST recover the version without writing only if the archive marker and API-key actor match

#### Scenario: A write throttle ends the current pass

- **GIVEN** an upload is rejected with a rate limit
- **WHEN** the client handles that response
- **THEN** it MUST return the typed throttle without another write in the same pass

### Requirement: Reconciliation is leased, paced, and resumable

Delivery reconciliation SHALL run as a leased worker job, with at most one pass in
flight for the instance. A pass SHALL be enqueued after a registry publication
completes, when revocations change, on the configured repair interval, and on
operator demand.

A pass that is interrupted, refused, or rate-limited MUST leave recorded per-plugin
progress intact so the next pass resumes rather than restarts. Refusal and
rate limiting MUST record a sanitized failure cause. Parent cancellation MUST
stop reconciliation without recording a per-plugin delivery failure.

When Claude already reports the expected version and installation preference,
delivery SHALL clear a stale failure without writing to Claude.

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

#### Scenario: Parent cancellation does not mark a plugin failed

- **GIVEN** an existing mirror with a published state
- **WHEN** its reconciliation context is canceled while reading the plugin
- **THEN** the pass MUST stop with the context error without changing the row

#### Scenario: A stale failure clears after read-only convergence

- **GIVEN** an unchanged mirror with a recorded failure whose remote preference already matches
- **WHEN** the next pass reads the remote state
- **THEN** delivery MUST clear the failure without sending a remote write
