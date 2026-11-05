## ADDED Requirements

### Requirement: Gemini API documentation supplies a model inventory

The audit SHALL monitor the Gemini API model documentation page as a model inventory source for the gemini harness. The parser SHALL extract model identifiers from that page and SHALL fail closed when no identifier is found.

#### Scenario: Gemini API page inventory lists a model absent from the catalog

- **WHEN** the Gemini API model documentation fixture lists `gemini-4-argon` and `gemini.yaml` does not contain `gemini-4-argon`
- **THEN** the audit SHALL report `catalog.model-missing` for `gemini-4-argon`

#### Scenario: Gemini API page inventory lists a model present in the catalog

- **WHEN** the Gemini API model documentation fixture lists `gemini-4-argon` and the catalog contains that identifier
- **THEN** the audit SHALL NOT report `catalog.model-missing` for `gemini-4-argon`

#### Scenario: Gemini API page without identifiers fails closed

- **WHEN** the Gemini API model documentation page contains no model identifiers
- **THEN** the audit SHALL report `source.inventory-unparsed` naming the Gemini API model documentation source

### Requirement: Unparseable model inventories are reported by name

The audit SHALL report `source.inventory-unparsed` naming the source when a model inventory source's page yields no parseable model list. A source with no parseable model list SHALL NOT pass silently.

#### Scenario: Inventory page without a model list

- **WHEN** an inventory source returns a page that contains no parseable model list
- **THEN** the audit SHALL report `source.inventory-unparsed` naming that source
- **AND** the source status SHALL be `unknown`

#### Scenario: Inventory page with a model list

- **WHEN** an inventory source returns a page with a parseable model list
- **THEN** the audit SHALL NOT report `source.inventory-unparsed` for that source

### Requirement: Family pins are flagged when a newer member is observed

The audit SHALL report `catalog.family-pin-stale` when a family pin names a model that is not the newest observed member of that family in the same source's inventory. Versions SHALL be compared numerically component by component, with missing trailing components treated as zero; a family member matches when its identifier contains the family name followed by a version. The audit SHALL NOT edit catalogs.

#### Scenario: Factory Haiku pin trails the observed Haiku

- **WHEN** the Factory inventory observes `claude-haiku-5-5` and the `anthropic/haiku` family pin names `claude-haiku-4-5-20251001`
- **THEN** the audit SHALL report `catalog.family-pin-stale` for `anthropic/haiku`

#### Scenario: Family pin is the newest observed member

- **WHEN** the family pin names the newest observed member of its family
- **THEN** the audit SHALL NOT report `catalog.family-pin-stale`

### Requirement: Devin model inventory is covered by the catalog

Every `model_uid` in the committed Devin CLI model snapshot SHALL be covered by a model or reasoning entry in `devin.yaml`. The coverage test SHALL fail for each uncovered UID and SHALL list the uncovered UIDs.

#### Scenario: Every snapshot UID is covered

- **WHEN** every `model_uid` in the Devin CLI model snapshot matches a `devin.yaml` model or reasoning entry
- **THEN** the coverage test SHALL pass

#### Scenario: Snapshot UID is not covered

- **WHEN** a `model_uid` in the Devin CLI model snapshot matches no `devin.yaml` model or reasoning entry
- **THEN** the coverage test SHALL fail and SHALL list that UID

### Requirement: Report schema stays at version one

The audit report SHALL remain schema version 1, and `summary` and `propose` SHALL accept reports containing the finding codes introduced by this capability.

#### Scenario: Summary accepts new finding codes

- **WHEN** a version 1 report contains `source.inventory-unparsed` or `catalog.family-pin-stale` findings
- **THEN** `summary` SHALL render those findings without error
