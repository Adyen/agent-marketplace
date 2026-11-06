## ADDED Requirements

### Requirement: Discovery YAML stays within parse ceilings

CLI skill discovery MUST report a skill whose YAML frontmatter exceeds 64 KiB as malformed, with an error identifying the frontmatter ceiling, without parsing it, and MUST still discover its siblings. Collection discovery MUST refuse a `COLLECTION.yaml` larger than 256 KiB before parsing it, with an error identifying the manifest ceiling. Manifests and frontmatter at or below the ceilings MUST load as before. Neither ceiling has an override.

#### Scenario: Oversized skill frontmatter is reported as malformed

- **WHEN** a registry contains a skill whose frontmatter is one byte above 64 KiB, a skill exactly at 64 KiB and an ordinary skill
- **THEN** discovery MUST report only the oversized skill as malformed with the frontmatter-ceiling error and MUST discover the other two

#### Scenario: Oversized collection manifest is refused before parsing

- **WHEN** a collection's `COLLECTION.yaml` is one byte above 256 KiB
- **THEN** collection discovery MUST fail with the manifest-ceiling error, and a manifest of exactly 256 KiB MUST load
