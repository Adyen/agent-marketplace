## MODIFIED Requirements

### Requirement: Current reported installation counts

The Marketplace SHALL derive adoption from distinct reporting Installation IDs whose latest complete inventory observation is no older than 30 days. A resource SHALL count at most once per Installation ID and resource coordinate. Only unambiguous matches between existing telemetry registry identities and Marketplace registry origins SHALL contribute. Counts describe enabled configuration, not verified materialization or unique users. Legitimate rule observations SHALL NOT contribute to adoption or invalidate counts for Marketplace-selectable resources. Superseded inventories SHALL NOT consume the metrics response budget when the latest-inventory projection fits within that budget.

#### Scenario: Latest inventory excludes removed and stale resources

- **WHEN** installation metrics include repeated reports, older inventories containing removed resources, and inventory observations older than 30 days
- **THEN** only resources present in each fresh latest inventory SHALL contribute once per reporting Installation ID
- **AND** fresh scrape timestamps SHALL NOT make an old inventory observation fresh

#### Scenario: Registry identity matching does not conflate resources

- **WHEN** installation metrics contain the same resource name in different registries, multiple origins belonging to one registry, or an origin matching multiple registry identities
- **THEN** counts SHALL remain separate for different resource coordinates and duplicate observations from one installation across one registry's origins SHALL count once
- **AND** ambiguous origins SHALL NOT contribute or produce fabricated zero counts

#### Scenario: Rule observations preserve selectable resource adoption

- **WHEN** a fresh latest inventory contains valid rule observations alongside Marketplace-selectable resources
- **THEN** installation-ordered search SHALL return counts for those selectable resources without counting rules
- **AND** malformed observations SHALL still trigger the existing metrics fallback

#### Scenario: Superseded inventory history does not exhaust the response budget

- **WHEN** superseded inventory history exceeds the metrics response budget but the latest-inventory projection fits within that budget
- **THEN** installation-ordered search SHALL return counts derived from the latest inventories
- **AND** equally recent conflicting inventory fingerprints SHALL remain ambiguous and SHALL NOT contribute
- **AND** responses exceeding the budget even after projection SHALL retain the existing discovery fallback
