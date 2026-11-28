# target-profile-import Specification

## Purpose
Local target-package import validation lets authors inspect a proposed package and destination plan before curation, without changing an installed setup or claiming that package code works at runtime.

## Requirements

### Requirement: Local target dry-run returns a versioned JSON report

The CLI SHALL provide `agent-marketplace target dry-run --profile <file> --source <dir> --import <file> [--destination <dir>]`. Its report SHALL use `schema_version: 1` and `status: valid` or `refused`, include `excluded_files` and `destination_check.ownership_changes`, and reserve `diagnostics` for problems.

#### Scenario: Valid local inputs produce a JSON import report

- **WHEN** an author supplies a valid profile and source containing two mapped files, a display file, and another unselected file, without a destination
- **THEN** the report MUST contain both mappings, `excluded_files` entries with reasons `display` and `not-selected`, `diagnostics: []`, and `destination_check` with `status: not-requested`, `conflicts: []`, and `ownership_changes: []`

#### Scenario: Refusal returns diagnostics and a nonzero exit

- **WHEN** profile, manifest, source, or destination validation refuses the import
- **THEN** the command MUST emit a report with `status: refused`, at least one stable problem diagnostic with the relevant source or destination path, an empty `destination_check.ownership_changes` array, and MUST exit nonzero

### Requirement: Import manifests and package identity are strict

The manifest SHALL be one strict YAML document with exact `apiVersion: agent-marketplace.io/target-import/v1` and exactly one each of `target_id`, `kind`, `name`, and non-empty `mappings`. Each mapping has only the required string fields `source_path` and `destination_path`. Duplicate keys, aliases/merge keys, unknown fields, and malformed values MUST refuse.

#### Scenario: Malformed import manifests refuse before accepting a package

- **WHEN** a manifest has a duplicate key, alias or merge key, unknown field, invalid API version, or malformed mapping values
- **THEN** the command MUST refuse with a stable diagnostic naming the manifest or offending field and MUST NOT accept any mapping

### Requirement: Package identity follows the target-profile name grammar

`target_id` and the local kind SHALL each be 1–64 ASCII bytes, start with lowercase `a`–`z`, and continue only with lowercase letters, digits, or `-`. `name` SHALL be 1–128 ASCII bytes, start with a lowercase letter or digit, and continue only with lowercase letters, digits, `.`, `_`, or `-`. `kind` SHALL be exactly `<target_id>/<local-kind>` and match the profile identity.

#### Scenario: Invalid or mismatched package identity refuses

- **WHEN** an identity uses disallowed characters or length, or its `target_id` or scoped `kind` does not match the profile
- **THEN** the command MUST refuse with a stable diagnostic naming the invalid identity or field

### Requirement: Selected payload mappings stay within package and profile boundaries

Each selected source file SHALL map exactly once to a unique destination permitted by a profile `into` path or exact `support_files` entry. Paths SHALL be unambiguous slash-separated relative paths without absolute prefixes, backslashes, empty, `.` or `..` components, or controls. Outside-root files, symlinks, special files, duplicate or missing mappings, and package-owned dependencies missing from payload MUST refuse with path-specific diagnostics.

#### Scenario: Unsafe or duplicate mappings refuse

- **WHEN** a selected mapping escapes the source root, uses an ambiguous or non-relative path, follows a symlink, selects a special file, or duplicates a source or destination
- **THEN** the command MUST refuse the affected package with a stable diagnostic naming the offending path

#### Scenario: Shipped names and missing packaged dependencies refuse

- **WHEN** the package claims a name the profile declares as shipped, or a required package-owned dependency is missing from the selected payload
- **THEN** the command MUST refuse the package with a stable diagnostic naming the package and missing or shipped path/identity

#### Scenario: Payload mappings preserve bytes and list source exclusions

- **WHEN** a valid import selects payload files and declares display files or leaves other source files excluded
- **THEN** the report MUST list each display file in `excluded_files` with reason `display`, each other non-selected source file with reason `not-selected`, and only selected files in `mappings`; payload bytes MUST remain identical to source bytes, and exclusions MUST NOT appear in `diagnostics`

#### Scenario: Source siblings are never imported

- **WHEN** the source directory contains another package beside the package named by the manifest
- **THEN** the report MUST describe only the manifest's package, list the sibling's files in `excluded_files` with reason `not-selected`, and omit them from its payload mappings

### Requirement: Dry-run reports advisory metadata without claiming runtime success

Optional `display_files` SHALL be a unique array of source-relative strings. `metadata.requires` SHALL accept `kind:name` strings or `{kind, name, registry?, path?}` objects, and `manual_setup` SHALL be an array of non-empty strings. The report SHALL separate these values from payload validation and runtime behavior.

#### Scenario: Unmet prerequisites and manual setup remain advisory

- **WHEN** a valid package declares a prerequisite that is not present and includes manual setup
- **THEN** the report MUST show the requirement as unmet and setup as pending, while keeping the import valid unless required package-owned payload is missing

### Requirement: Destination preview is read-only and uses place collision rules

When supplied, `--destination` SHALL name an existing canonical destination admitted by the profile. The command MUST preview conflicts and ownership using the same rules as place delivery, MUST refuse unowned collisions even when bytes are identical, and MUST NOT create or repair destination or ownership state. `ownership_changes` SHALL list only predicted ownership transitions; a new claim uses `{destination_path, action: claim}` and an unchanged already-owned path is omitted.

#### Scenario: Destination collision refuses without adopting identical bytes

- **WHEN** one mapped path in a requested destination contains an unowned file with identical bytes while another mapped path could otherwise be claimed
- **THEN** the report MUST identify the collision, refuse the whole package, set `destination_check.ownership_changes` to `[]`, and leave the existing file and ownership state unchanged

#### Scenario: New destination paths report predicted claims

- **WHEN** an existing canonical destination admitted by the profile is supplied and mapped paths require new ownership
- **THEN** each predicted new claim MUST appear in `destination_check.ownership_changes` with its destination path and `action: claim`, without publishing files or ownership state

#### Scenario: Unchanged owned paths are not new claims

- **WHEN** a supplied destination already owns a mapped path and its bytes and mode are unchanged
- **THEN** that path MUST be omitted from `destination_check.ownership_changes`

#### Scenario: Refused whole-package preview has no ownership changes

- **WHEN** any mapped path causes a destination preview to refuse the package
- **THEN** `destination_check.ownership_changes` MUST be `[]` even if other mapped paths could individually be claimed

#### Scenario: Dry-run leaves all existing state unchanged

- **WHEN** a valid import is previewed with or without an admitted destination, or validation refuses
- **THEN** the command MUST NOT change or create target files, consumer configuration, acquisition locks, approval, ownership state, cache, registry data, or other application state

#### Scenario: Local dry-run needs no configuration, approval, acquisition, or host execution

- **WHEN** valid local profile, source, and import files are available without organization configuration, approval, or an installation
- **THEN** the command MUST complete using only those local inputs, MUST NOT acquire a source or acquisition lock, MUST fetch or create no locks, MUST run no probes, host commands, package code, substitutions, or installers, and MUST NOT claim that the package works at runtime
