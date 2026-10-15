# plugin-policy-catalogs Specification

## Purpose
Defines the shipped plugin policy catalogs: embedded per-harness and
per-package-format data that records which plugin package layouts each
harness accepts, whether decomposed and native plugin delivery are
verified, and the evidence behind each claim. Governs catalog validation
and the generated plugin support reference.

## Requirements

### Requirement: Plugin catalogs are shipped, validated data

Every registered harness target and every recognized package format SHALL
ship an embedded plugin catalog consumed by the offline catalog check.
Malformed catalog data MUST fail the check. Plugin delivery and
plugin-surface field handling SHALL be decided only from these catalogs.

#### Scenario: Every harness target and package format ships a plugin catalog

- **WHEN** the catalog check loads the shipped plugin catalogs
- **THEN** every registered harness target and every recognized package
  format MUST have exactly one

#### Scenario: Malformed plugin catalogs fail the check

- **WHEN** a plugin catalog carries an unknown key, a duplicate key, a field
  outcome outside the allowed set, a blocking outcome without a diagnostic
  code, an unknown policy or format reference, or no evidence refs
- **THEN** the catalog check MUST fail and name the file and field

### Requirement: Native delivery is declared only with probe evidence

A harness catalog SHALL declare native delivery as verified only together
with the identifier of a native probe present in the harness validation
suite, and only for a package format the harness accepts.

#### Scenario: Verified native delivery without a probe fails the check

- **WHEN** a harness catalog declares native delivery verified with no probe
  identifier, or with a probe identifier the harness validation suite does
  not define
- **THEN** the catalog check MUST fail

#### Scenario: Verified native delivery for an unaccepted format fails the check

- **WHEN** a harness catalog declares native delivery verified for a package
  format the same catalog does not accept
- **THEN** the catalog check MUST fail

### Requirement: The plugin support reference is generated from the catalogs

The public plugin support reference SHALL be generated from the shipped
plugin catalogs and SHALL list, per harness target, the accepted package
formats, the decomposed and native delivery status, and the evidence date.
The catalog check MUST fail when the committed reference is stale.

#### Scenario: Stale plugin support reference fails the check

- **WHEN** a plugin catalog changes and the committed reference is not
  regenerated
- **THEN** the catalog check MUST fail

### Requirement: Conversion fixtures prove plugin catalog policies

The catalog check SHALL run hand-written plugin conversion fixtures through
the public package compilation path, and each fixture's expected projection
and diagnostics MUST match.

#### Scenario: A plugin fixture mismatch fails the check

- **WHEN** a plugin catalog edit changes the projection of a member covered
  by a conversion fixture
- **THEN** the catalog check MUST fail and report the fixture
