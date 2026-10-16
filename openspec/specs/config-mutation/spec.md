# config-mutation Specification

## Purpose
Defines how CLI edits create and write the user, repository, and personal-local configuration layer roots.

## Requirements

### Requirement: CLI edits create a missing configuration root

When a CLI edit targets the user, repository, or personal-local layer and that layer's root config file does not exist, the edit SHALL create the root as a v4 configuration and apply the edit to it. The edit MUST succeed whether or not the root's directory exists, including when the home directory is reached through the macOS `/tmp`, `/var`, or `/etc` alias into `/private`. A new user root SHALL declare the harness targets `factory` and `windsurf`, the list `init` writes.

#### Scenario: User-scope install creates a missing user root

- **WHEN** `install <name> --scope user` runs for a resource from an ad hoc `--from` registry or from a source in a v4 repository configuration, and `~/.agents` does not exist
- **THEN** the command MUST exit 0, create `~/.agents/config.yaml` as a v4 root that selects the resource, materialize the resource, and leave `sync --check` in sync

#### Scenario: New user root declares the default harness targets

- **WHEN** a user-scope install creates the user root and no configuration layer declares harness targets
- **THEN** the new root MUST declare `harnesses: [factory, windsurf]` and the resource MUST be materialized for those harness targets

#### Scenario: Marketplace install creates a missing user root and its acquisition lock

- **WHEN** a Marketplace resource is installed with `--scope user` and `~/.agents` does not exist
- **THEN** the command MUST create a v4 user root with a `marketplace` source and the user acquisition lock `~/.agents/lock.yaml` recording the resource

### Requirement: CLI edits never write through a dangling symlink

A CLI edit SHALL refuse a target layer whose root config file, or a directory above it, is a symlink to a missing target, and MUST NOT create or change the symlink or anything at its target. Read-only commands and `sync` SHALL treat such a layer as absent.

#### Scenario: User-scope install through a dangling symlink is refused

- **WHEN** `~/.agents` or `~/.agents/config.yaml` is a symlink to a missing target and a Git or Marketplace resource is installed with `--scope user`
- **THEN** the command MUST exit non-zero with an error that names the symlink condition, and the symlink and its missing target MUST remain unchanged

#### Scenario: Dangling user config reads as absent

- **WHEN** `~/.agents` or `~/.agents/config.yaml` is a symlink to a missing target
- **THEN** `status` and `sync` MUST exit 0 and MUST NOT create or change the symlink or its target

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
