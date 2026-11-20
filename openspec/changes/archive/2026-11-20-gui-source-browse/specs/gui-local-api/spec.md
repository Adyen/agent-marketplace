## ADDED Requirements

### Requirement: Marketplace endpoint source association

The local API SHALL provide a consistent source association between a configured Marketplace endpoint and its attributable verified and discovered resource rows without disclosing redacted endpoint data. It SHALL use verified identity rather than a display name or a registry label. Rows with no reliable association SHALL NOT be advertised as matching a configured endpoint. This association SHALL NOT alter the source labels or registry grouping used for display.

#### Scenario: Marketplace source association distinguishes equal labels

- **WHEN** two distinct configured Marketplace endpoints have the same display label and each contributes resources
- **THEN** the local API MUST allow the GUI to associate each attributable resource only with its own endpoint, without exposing redacted endpoint data or relying on display labels
