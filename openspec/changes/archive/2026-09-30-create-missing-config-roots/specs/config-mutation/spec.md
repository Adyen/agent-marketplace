## Purpose

Defines how CLI edits create and write the user, repository, and personal-local configuration layer roots.

## ADDED Requirements

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
