# catalog-discovery Specification

## Purpose
Describe ignored nested bundles with bounded resource examples and actionable
registry context while preserving captured source authority during discovery.

## Requirements

### Requirement: Ignored-sub-bundle warnings identify affected resources and remediation

When root resources prevent independent nested bundles from being discovered, Agent Marketplace SHALL retain the `catalog.discover.sub-bundles-ignored` diagnostic and describe the ignored resources using registry-relative paths, quoted names, and resource kinds. The warning SHALL identify an example root resource to move and explain how to restructure the root or target an ignored bundle directly. Detailed examples SHALL retain the admitted source root's authority and SHALL NOT read a replacement root.

#### Scenario: Mixed root warning identifies resources and remediation

- **WHEN** a root contains a command and an independent nested bundle containing a skill
- **THEN** discovery SHALL retain only the root's resources and report the ignored bundle's relative path and affected resource
- **AND** the warning SHALL identify the root command and its relative path
- **AND** it SHALL suggest moving root resources into a sub-bundle such as `common/` or appending the ignored bundle's relative path to the registry entry path
- **AND** detailed examples SHALL NOT expose temporary absolute paths

#### Scenario: Ignored bundle warning identifies its registry entry

- **WHEN** an ignored-sub-bundle warning is returned while loading a registry entry
- **THEN** it SHALL include the registry URL, ref, and entry path
- **AND** adding registry context SHALL preserve the underlying diagnostic cause

#### Scenario: Ignored bundle details are sorted and bounded

- **WHEN** six ignored bundles each contain six resources
- **THEN** the warning SHALL report the total ignored-bundle count, detail at most five bundles, and report the omitted bundle count
- **AND** it SHALL list at most five resource examples per detailed bundle sorted by resource kind and name, with the omitted resource count

#### Scenario: Ignored bundle warning retains captured source authority

- **WHEN** a captured source root containing root resources and an ignored nested bundle is renamed and its configured path is replaced
- **THEN** discovery SHALL retain the captured root resource and report the original ignored bundle
- **AND** the warning SHALL NOT import bundle paths or resource names from the replacement root

### Requirement: Discovery YAML stays within parse ceilings

CLI skill discovery MUST report a skill whose YAML frontmatter exceeds 64 KiB as malformed, with an error identifying the frontmatter ceiling, without parsing it, and MUST still discover its siblings. Collection discovery MUST refuse a `COLLECTION.yaml` larger than 256 KiB before parsing it, with an error identifying the manifest ceiling. Manifests and frontmatter at or below the ceilings MUST load as before. Neither ceiling has an override.

#### Scenario: Oversized skill frontmatter is reported as malformed

- **WHEN** a registry contains a skill whose frontmatter is one byte above 64 KiB, a skill exactly at 64 KiB and an ordinary skill
- **THEN** discovery MUST report only the oversized skill as malformed with the frontmatter-ceiling error and MUST discover the other two

#### Scenario: Oversized collection manifest is refused before parsing

- **WHEN** a collection's `COLLECTION.yaml` is one byte above 256 KiB
- **THEN** collection discovery MUST fail with the manifest-ceiling error, and a manifest of exactly 256 KiB MUST load
