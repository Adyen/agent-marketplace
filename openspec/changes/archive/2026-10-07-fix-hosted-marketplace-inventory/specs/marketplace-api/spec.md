## MODIFIED Requirements

### Requirement: Current reported installation counts

The Marketplace SHALL derive adoption from distinct reporting Installation IDs whose latest complete inventory observation is no older than 30 days. A resource SHALL count at most once per Installation ID and resource coordinate, including observations of the same coordinate through both Git and Marketplace acquisition. Legacy Git observations SHALL contribute only through unambiguous matches between telemetry registry identities and Marketplace registry origins. Explicit hosted observations SHALL contribute only when their Marketplace instance ID matches the serving Marketplace and their registry ID is known to that Marketplace. Counts describe enabled configuration, not verified materialization or unique users. Legitimate rule observations SHALL NOT contribute to adoption or invalidate counts for Marketplace-selectable resources. Superseded inventories SHALL NOT consume the metrics response budget when the latest-inventory projection fits within that budget.

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

## ADDED Requirements

### Requirement: Complete hosted inventory reporting

Detailed primary telemetry SHALL represent resolved Marketplace resources using explicit source.kind=`marketplace`, marketplace.id, registry.id, resource.kind and canonical resource.name attributes. Legacy Git and directory observations SHALL remain readable without the new attributes. Root and pathful Marketplace endpoints, mirror acquisition, index-only acquisition and installed aliases SHALL NOT cause valid complete inventories to be omitted. Inventory registry and resource points SHALL retain hosted identity without exporting endpoint credentials or private source paths. Attribute values SHALL obey the existing 512-byte UTF-8 bound and total inventories SHALL obey the existing 1000-registry and 10000-resource bounds. Unknown source kinds, incoherent hosted identity pairs, malformed identifiers and exceeded bounds SHALL retain whole-generation omission or metrics-unavailable fallback rather than silently publishing incomplete data. Basic and disabled telemetry SHALL NOT emit resource inventories.

#### Scenario: Mixed hosted and Git inventory remains complete

- **WHEN** detailed telemetry observes a successful configuration containing Git resources and valid Marketplace resources acquired through root or pathful endpoints using mirror or index-only acquisition
- **THEN** the complete inventory SHALL include both types of resource with separate identity attributes
- **AND** hosted resources SHALL use their canonical coordinate names rather than installed aliases
- **AND** the original acquisition and ownership identities SHALL remain unchanged

#### Scenario: Hosted wire identity survives detailed export

- **WHEN** a valid complete inventory with hosted resources is exported to the primary OTLP endpoint
- **THEN** registry and resource observations SHALL retain source.kind and marketplace.id and the snapshot SHALL share their configuration fingerprint
- **AND** legacy-only observations SHALL omit the new identity attributes
- **AND** basic or disabled telemetry SHALL emit no inventory

#### Scenario: Invalid hosted identity preserves safe omission

- **WHEN** a hosted identity is missing required coordinates, contains malformed or oversized values, or exceeds the inventory cardinality bounds
- **THEN** no partial complete inventory SHALL be emitted and a bounded omission reason SHALL be recorded when detailed telemetry is active
- **AND** a metrics response with an unknown source kind or incoherent hosted identity attributes SHALL retain the existing unavailable-count fallback
