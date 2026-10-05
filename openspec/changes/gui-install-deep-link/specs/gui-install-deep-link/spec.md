## Purpose

Defines how Marketplace resource links are validated, resolved, reviewed, and handed to the desktop GUI without installing anything before explicit user confirmation.

## ADDED Requirements

### Requirement: Validated install-link format

The GUI SHALL accept install links in the form `agent-marketplace://install?marketplace=<Marketplace endpoint URL>&resource=<resource coordinate>[&digest=<resource revision content digest>]`. A supplied digest SHALL select that exact Resource revision; without a digest, the GUI SHALL resolve the latest installable Resource revision. All parameters SHALL be treated as untrusted. Malformed, oversized, or unknown-parameter links SHALL be rejected with a visible reason and no side effects.

#### Scenario: Exact and latest revisions are selected by the link

- **WHEN** a valid install link contains a digest or omits the optional digest
- **THEN** the GUI SHALL resolve the exact Resource revision named by the digest or the latest installable revision when no digest is supplied

#### Scenario: Invalid link parameters are rejected

- **WHEN** an install link is malformed, oversized, or contains an unknown parameter
- **THEN** the GUI SHALL display a rejection reason
- **AND** it SHALL perform no network resolution, configuration write, or Resource installation

### Requirement: Explicit install confirmation

The GUI SHALL install nothing without explicit confirmation. Before confirmation, the GUI SHALL show the Marketplace, Resource coordinate, Resource revision, user scope, target Harness targets, and the full plan including dependencies and collection members. For hooks and MCP servers, the confirmation SHALL state that the Resource runs commands on this machine and show the command. Cancelling SHALL write nothing.

#### Scenario: Confirmation shows the complete plan

- **WHEN** a valid link resolves to an installable Resource and plan
- **THEN** confirmation SHALL show the Marketplace, Resource coordinate, Resource revision, user scope, target Harness targets, dependencies, and collection members
- **AND** for a hook or MCP server it SHALL show the command and warn that the Resource runs commands on this machine
- **AND** the Resource SHALL remain uninstalled until the user explicitly confirms

#### Scenario: Cancelling confirmation writes nothing

- **WHEN** a user cancels the install confirmation
- **THEN** no Resource SHALL be installed and no configuration SHALL be written

### Requirement: Marketplace verification and policy refusals

The GUI SHALL verify the exact Resource revision against the Marketplace before installation. Revoked revisions and Resources blocked by Marketplace security policy SHALL be refused with the same concise reason shown by the Marketplace web UI.

#### Scenario: Revoked or policy-blocked revision is refused

- **WHEN** the exact revision is revoked or the Resource is blocked by Marketplace security policy
- **THEN** the GUI SHALL refuse installation with the Marketplace web UI's concise reason
- **AND** no configuration or materialized Resource SHALL be written

#### Scenario: Exact revision is checked before installation

- **WHEN** a user confirms installation of a link containing an exact digest
- **THEN** the GUI SHALL verify that exact Resource revision against the Marketplace before the guarded installation

### Requirement: Unregistered Marketplace handling

When a link targets a Marketplace endpoint not registered in effective configuration, the GUI SHALL refuse it if the effective `allowed_marketplaces` allowlist blocks its URL. Otherwise, the GUI SHALL warn that the endpoint is unregistered and may offer to add a Marketplace endpoint registration to the user-personal root after verifying endpoint identity. Declining registration SHALL write nothing, and every installation SHALL still require the explicit install confirmation.

#### Scenario: Allowlist blocks an unregistered Marketplace

- **WHEN** a link targets an unregistered Marketplace endpoint whose URL is blocked by the effective allowlist
- **THEN** the GUI SHALL refuse the link without writing configuration or installing a Resource

#### Scenario: User declines an unregistered Marketplace

- **WHEN** the endpoint is not registered, is not blocked by the allowlist, and the user declines the registration offer
- **THEN** no Marketplace endpoint registration or Resource installation SHALL be written

#### Scenario: Verified unregistered Marketplace can be added before review

- **WHEN** the endpoint is not registered, is not blocked by the allowlist, and its identity is verified
- **THEN** the GUI MAY offer to add its Marketplace endpoint registration to the user-personal root
- **AND** any Resource installation SHALL still require the complete explicit install confirmation

### Requirement: Link launch and readiness

A valid link SHALL launch the GUI when it is not running or be handed to its running instance. If first-run setup is incomplete, the GUI SHALL complete first run and then present the pending install confirmation.

#### Scenario: Link launches or reaches the running GUI

- **WHEN** a valid install link is opened while the GUI is stopped or already running
- **THEN** it SHALL launch the GUI or be handed to the existing instance
- **AND** the link SHALL be presented for resolution and confirmation

#### Scenario: First run precedes pending install confirmation

- **WHEN** a valid install link is opened before first-run setup is complete
- **THEN** the GUI SHALL complete first run before presenting the pending install confirmation

### Requirement: Busy link rejection

A link received while sync, update, install, or remove is running SHALL be rejected with a busy notice and SHALL NOT be retained.

#### Scenario: Busy operation discards the link

- **WHEN** a link is received while sync, update, install, or remove is active
- **THEN** the GUI SHALL show a busy notice
- **AND** it SHALL discard the link without queuing or later installing it
