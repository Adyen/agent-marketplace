## MODIFIED Requirements

### Requirement: Current reported installation counts

The Marketplace SHALL derive adoption from distinct reporting Installation IDs whose latest complete inventory observation is no older than 30 days. A resource SHALL count at most once per Installation ID and resource coordinate, including observations of the same coordinate through both Git and Marketplace acquisition. Legacy Git observations SHALL contribute only through unambiguous matches between telemetry registry identities and Marketplace registry origins. Explicit hosted observations SHALL contribute only when their Marketplace instance ID matches the serving Marketplace and their registry ID is known to that Marketplace. Counts describe enabled configuration, not verified materialization or unique users. Legitimate rule observations SHALL NOT contribute to adoption or invalidate counts for Marketplace-selectable resources. Superseded inventories SHALL NOT consume the metrics response budget when the latest-inventory projection fits within that budget. The metrics backend SHALL evaluate these rules and return aggregate counts per counted resource coordinate together with the fresh-installation count and the oldest fresh observation, so the metrics response grows with counted coordinates rather than with reporting installations. Well-formed metrics responses SHALL NOT contain Installation IDs or configuration fingerprints.

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

#### Scenario: Hosted adoption is isolated and deduplicated

- **WHEN** a fresh complete inventory reports hosted resource coordinates from the serving Marketplace, another Marketplace with the same registry and resource names, and a Git observation matching an already reported hosted coordinate
- **THEN** installation-ordered search SHALL count only the serving Marketplace's coordinates and count the shared Git and hosted coordinate once per Installation ID
- **AND** different registry IDs SHALL remain distinct even when resource names match
- **AND** a newer complete generation without the resource SHALL remove its contribution

#### Scenario: Adoption growth does not exhaust the response budget

- **WHEN** many fresh installations report the same resource coordinates, alongside local and unknown registry observations, so that per-installation latest inventories would exceed the metrics response budget
- **THEN** the metrics response SHALL contain one count per counted resource coordinate, one fresh-installation count and one oldest fresh observation, and installation-ordered search SHALL return those counts
- **AND** the response SHALL contain no Installation ID, configuration fingerprint, local registry or unknown registry observation
- **AND** no fresh installation SHALL leave counts unavailable rather than fabricate zero values

#### Scenario: Malformed observations retain the fallback under aggregation

- **WHEN** latest inventories contain an unknown source kind, an incoherent hosted identity pair, an invalid hosted identity, an unknown resource kind, an empty registry or resource name, a non-positive, non-integer or oversized snapshot value, or a value other than one on an observation that would contribute to counts
- **THEN** installation-ordered search SHALL use the existing unavailable-count fallback
- **AND** well-formed hosted identities that merely resemble violations SHALL NOT cause the fallback
