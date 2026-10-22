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
