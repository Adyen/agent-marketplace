# gui-local-api Specification

## Purpose
Defines the authenticated local boundary between the desktop GUI and its interactive session, including transport security, the generated API contract, ordered events, and serialized mutations.

## Requirements

### Requirement: Credentialed local access

The GUI local API SHALL accept requests only from the GUI's own window and its test harness. Every request, including event-stream requests, without the per-launch credential SHALL be rejected without side effects. The credential SHALL be random for each launch, SHALL NOT be persisted, and SHALL NOT be written to logs or telemetry.

#### Scenario: Requests without the launch credential are rejected

- **WHEN** the GUI window or test harness sends an API or event-stream request without a valid per-launch credential
- **THEN** the request SHALL be rejected before any session read or mutation has side effects
- **AND** the credential SHALL NOT appear in persistent state, logs, or telemetry

#### Scenario: A credential from an earlier launch is invalid

- **WHEN** a request uses a credential issued by a previous GUI launch
- **THEN** the request SHALL be rejected without side effects

### Requirement: Loopback-only and rebinding-safe access

When the local API uses a socket, it SHALL listen only on the loopback interface at an ephemeral port. It SHALL reject requests whose Host is not the loopback address and port or whose Origin is not the GUI's own origin, without side effects, and SHALL NOT grant cross-origin resource sharing.

#### Scenario: Non-loopback Host or foreign Origin is rejected

- **WHEN** a request reaches the socket with a Host other than its loopback address and port or an Origin other than the GUI's own origin
- **THEN** the request SHALL be rejected without session side effects
- **AND** no cross-origin resource sharing permission SHALL be returned

#### Scenario: Socket binds only to loopback at an ephemeral port

- **WHEN** the GUI starts the local API using a socket
- **THEN** the listener SHALL bind only to a loopback interface and a port selected for that launch

### Requirement: Published API contract

The local API SHALL have a checked-in OpenAPI contract from which the GUI client is generated. A drift check SHALL fail when the generated client and the contract diverge.

#### Scenario: Generated client matches the published contract

- **WHEN** the drift check runs against a client generated from the checked-in OpenAPI contract
- **THEN** it SHALL report no drift
- **AND** a client or contract change that is not regenerated SHALL make the check fail

### Requirement: Ordered session events

Snapshot stages, progress observations, and notifications SHALL be delivered as an ordered event stream carrying the applicable `session-startup` or `session-feedback` generation and sequence identity. A client SHALL be able to discard stale, duplicate, and out-of-order events under those capabilities' rules.

#### Scenario: Event identity preserves order and generation

- **WHEN** snapshot stages, progress observations, and notifications are delivered during a session
- **THEN** each event SHALL carry its applicable generation and sequence identity and the stream SHALL preserve event order
- **AND** a client SHALL be able to reject a stale or out-of-order event without replacing newer state

### Requirement: Single-writer mutations

Mutating API calls SHALL use the same guarded operation as the TUI under `session-interactions`. A mutation requested while another guarded operation is running SHALL be rejected as busy without side effects.

#### Scenario: Concurrent mutation is rejected as busy

- **WHEN** a mutating API call arrives while another guarded operation is active
- **THEN** it SHALL be rejected as busy without changing configuration or materialized resources

### Requirement: Early target harnesses

Once the configuration layers validate, the GUI snapshot stream SHALL report the configured target harnesses with the configuration stage, without waiting for catalog acquisition or inventory resolution. Later stages SHALL report the same target set unless the configuration changed.

#### Scenario: Configuration stage reports target harnesses

- **WHEN** startup's catalog acquisition has not finished
- **THEN** the configuration-stage snapshot SHALL already list the configured target harnesses

### Requirement: Snapshot row source identity

Each snapshot row SHALL carry a source identity and a display label. The identity SHALL distinguish normalized registry URL, ref, and normalized configured registry subpath, and SHALL be `local` for a row without a registry URL. The label SHALL be the registry display name or URL-derived repository name with the full normalized subpath, or `(local)` without registry metadata, and SHALL be sanitized of terminal control data.

#### Scenario: Rows from distinct sources carry distinct identities

- **WHEN** the snapshot contains rows from sources that share a registry name but differ in ref or subpath, a row without a registry URL, and a registry name containing control characters
- **THEN** rows from different refs or subpaths SHALL carry different identities with the shared name and subpath as labels, the local row SHALL carry identity `local` and label `(local)`, and no label SHALL contain control characters

### Requirement: Prompt session event delivery in the desktop window

The desktop window SHALL receive each session event as soon as the local API emits it, without waiting for a later request, user action, or event. The window's event stream SHALL carry the same ordered events, generation, and sequence identity as the HTTP event stream, and SHALL require the per-launch credential.

#### Scenario: Completion notification renders without further activity

- **WHEN** a Sync or Update settles in the desktop app and no further request, user action, or event follows
- **THEN** its final snapshot and completion notification MUST reach the window, and the notification MUST render without further activity

#### Scenario: Window event stream requires the credential

- **WHEN** the window opens a session event stream without the per-launch credential
- **THEN** no session event MUST be delivered on that stream, and the window MUST receive an unauthorized refusal

### Requirement: Typed operation notification outcomes
Every local API notification payload SHALL include an outcome of success, warning or failure alongside its severity and message. The generated GUI client SHALL agree with the API contract. The GUI SHALL choose notification tone from the outcome, not message wording.

#### Scenario: Outcome drives notification tone
- **WHEN** success, warning and failure notification payloads reach the GUI with messages that do not end in " complete"
- **THEN** their tones SHALL be success, warning and danger respectively
- **AND** the API contract and generated client SHALL declare the same outcome values

### Requirement: Named operation completion messages
Operation notifications SHALL name the completed action or its failure without the word "mutation". Resource messages SHALL include the display name. Successful operations with warnings SHALL append " with N warning(s)" to their success message and carry the warning outcome. Failed operations SHALL carry the failure outcome even when warnings are present.

#### Scenario: Resource notifications name action and resource
- **WHEN** an install, remove, exclude or restore finishes successfully, fails, or succeeds with warnings
- **THEN** its message SHALL be "Installed <displayName>", "Removed <displayName>", "Excluded <displayName>" or "Restored <displayName>" on success, and "Couldn't install <displayName>", "Couldn't remove <displayName>", "Couldn't exclude <displayName>" or "Couldn't restore <displayName>" on failure
- **AND** success with warnings SHALL append " with N warning(s)"

#### Scenario: Configuration and lifecycle notifications name what changed
- **WHEN** adding a source, removing a source, another configuration change, Sync, Update or saving harness targets settles
- **THEN** success messages SHALL be "Source added", "Source removed", a name for the other changed setting, "Sync complete", "Update complete" or "Harness targets saved" respectively
- **AND** failures SHALL read "Couldn't add source", "Couldn't remove source", a named configuration failure, "Sync failed", "Update failed" or "Couldn't save harness targets" respectively
- **AND** no notification SHALL contain "mutation"
