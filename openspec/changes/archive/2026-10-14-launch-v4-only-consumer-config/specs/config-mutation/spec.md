## MODIFIED Requirements

### Requirement: Managed layer publication retains scoped authority

Supported edits and dependency reconciliation for user, repository and personal-local config/lock destinations MUST publish through captured filesystem descriptors. For new work, consumer input MUST first satisfy `consumer-config-format`; no consumer migration operation is provided. Repository and personal-local links MUST remain contained within their authorized layer root; user dotfile links MAY target current-user-owned singly linked regular files outside the user root. Approved links MUST remain links while their captured target receives the intended update. Unsafe ancestors, escaping or dangling publication links, special files, multiply linked targets and distinct layer paths aliasing one object MUST be refused before publication. Refusal MUST preserve external bytes, identity, mode, modification time and link text, and remove only staging residue created by the failed attempt. A config content link MUST NOT relocate the acquisition lock out of its logical layer. Existing missing-root creation, read-only dangling-layer and pending-transaction recovery contracts SHALL remain unchanged.

#### Scenario: Repository edit refuses an external configuration target

- **WHEN** a repository config root links to a valid v4 configuration outside its authorized root and a harness edit is requested
- **THEN** the edit MUST return a typed unsafe-path refusal
- **AND** external configuration bytes, identity, permissions, modification time, the link and staging state MUST remain unchanged

#### Scenario: Approved layer links receive real updates

- **WHEN** a supported v4 install, harness edit or lock reconciliation targets an approved user dotfile or contained repository/personal-local config/lock link
- **THEN** the intended update MUST occur at the captured target while the logical link and existing permissions remain intact
- **AND** the acquisition lock MUST remain owned by the logical config layer

#### Scenario: Replaced publication parents and leaves are refused

- **WHEN** a config/lock destination parent or leaf is replaced after capture and before staging or publication
- **THEN** publication MUST return a typed generation conflict without changing the replacement tree or captured tree
- **AND** attempted staging residue MUST be removed

#### Scenario: Cross-layer object aliases are refused

- **WHEN** distinct managed config/lock layer paths participating in a transaction resolve to one filesystem object
- **THEN** the transaction MUST refuse before any new config, lock, journal or staging mutation

#### Scenario: Purge removes an approved layer link without deleting its target

- **WHEN** user purge-config targets an approved dotfile link
- **THEN** only the managed logical link MUST be removed and its target bytes, identity, permissions and modification time MUST remain unchanged
