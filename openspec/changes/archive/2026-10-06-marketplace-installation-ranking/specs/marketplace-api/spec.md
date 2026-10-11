## ADDED Requirements

### Requirement: Current reported installation counts

The Marketplace SHALL derive adoption from distinct reporting Installation IDs whose latest complete inventory observation is no older than 30 days. A resource SHALL count at most once per Installation ID and resource coordinate. Only unambiguous matches between existing telemetry registry identities and Marketplace registry origins SHALL contribute. Counts describe enabled configuration, not verified materialization or unique users.

#### Scenario: Latest inventory excludes removed and stale resources

- **WHEN** installation metrics include repeated reports, older inventories containing removed resources, and inventory observations older than 30 days
- **THEN** only resources present in each fresh latest inventory SHALL contribute once per reporting Installation ID
- **AND** fresh scrape timestamps SHALL NOT make an old inventory observation fresh

#### Scenario: Registry identity matching does not conflate resources

- **WHEN** installation metrics contain the same resource name in different registries, multiple origins belonging to one registry, or an origin matching multiple registry identities
- **THEN** counts SHALL remain separate for different resource coordinates and duplicate observations from one installation across one registry's origins SHALL count once
- **AND** ambiguous origins SHALL NOT contribute or produce fabricated zero counts

### Requirement: Optional popularity search order

The Marketplace search API SHALL accept `sort=installations` to order the complete filtered result by descending reported installation count before pagination. For text searches, existing relevance SHALL precede installation count. Equal ranks SHALL retain deterministic registry identity, resource kind, and name ordering. Requests omitting `sort` SHALL retain existing ordering and SHALL NOT require a metrics backend. Unsupported sort values SHALL return HTTP 400.

#### Scenario: Popularity applies before pagination with relevant search matches

- **WHEN** a client requests installation ordering with pagination, text, kind, or registry filters
- **THEN** installation rank SHALL apply to the full filtered result before selecting a page, after text relevance when present
- **AND** ties SHALL be deterministic and existing filter semantics SHALL remain unchanged

#### Scenario: Existing clients and invalid sorting remain predictable

- **WHEN** a client omits `sort` or supplies an unsupported sort value
- **THEN** an omitted value SHALL preserve existing search ordering without contacting metrics
- **AND** an unsupported value SHALL return HTTP 400

### Requirement: Optional bounded installation metrics connection

Marketplace operator configuration SHALL optionally select a Prometheus-compatible metrics endpoint, deployment label selectors, and an environment-sourced bearer credential. The endpoint and credentials SHALL remain server-side. Metrics requests SHALL have bounded execution and response size and SHALL NOT forward credentials through redirects. Successful aggregate counts SHALL be cached for five minutes. Disabled, empty, failed, malformed, or expired metrics SHALL preserve existing search ordering and omit installation counts. Public responses and logs SHALL NOT disclose Installation IDs or metrics credentials.

#### Scenario: Metrics unavailable does not break resource discovery

- **WHEN** metrics are disabled, empty, failed, malformed, or an expired cached result cannot be refreshed
- **THEN** resource search SHALL remain available in its existing order
- **AND** public resource items SHALL omit installation counts rather than fabricate zero values or reuse expired counts

#### Scenario: Metrics configuration and requests preserve the server boundary

- **WHEN** an operator configures metrics or the Marketplace queries its metrics endpoint
- **THEN** invalid endpoint or selector configuration SHALL be rejected, requests and responses SHALL be bounded, and redirects SHALL NOT receive credentials
- **AND** repeated searches within five minutes of a successful refresh SHALL reuse aggregate counts without repeating the metrics request
- **AND** public responses and logs SHALL omit Installation IDs and credentials
