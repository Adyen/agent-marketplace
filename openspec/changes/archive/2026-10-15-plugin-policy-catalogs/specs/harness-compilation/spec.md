## ADDED Requirements

### Requirement: A Factory plugin root skill is not dropped silently

Factory Droid plugins load skills only from `skills/<name>/SKILL.md`. When a
Factory plugin-form source (one with `.factory-plugin/plugin.json`) has a
`SKILL.md` at its root, ingestion SHALL load no skill from that file and
SHALL emit a warning diagnostic with code `ingest.factory.root-skill-ignored`
that names the file and states that Factory plugins load skills only from
`skills/<name>/`.

#### Scenario: Root skill without a skills tree warns

- **WHEN** a Factory plugin-form source has a root `SKILL.md` and no
  `skills/` directory
- **THEN** ingestion MUST load no skill from the root `SKILL.md`
- **AND** it MUST emit an `ingest.factory.root-skill-ignored` warning naming
  the file

#### Scenario: Root skill beside a skills tree warns and keeps the tree

- **WHEN** a Factory plugin-form source has a root `SKILL.md` and skills
  under `skills/<name>/`
- **THEN** ingestion MUST load every skill under `skills/`
- **AND** it MUST emit an `ingest.factory.root-skill-ignored` warning naming
  the root file and load no skill from it
