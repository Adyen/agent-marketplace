## ADDED Requirements

### Requirement: Malformed rows keep their registry

A malformed resource row SHALL carry the registry metadata of the configured
source whose catalog reported it, so its label uses that registry's display
name like any other row, its detail names the registry, and same-named
malformed resources from different sources remain distinct rows with distinct
identities. Only a malformed resource whose catalog matches no configured
source SHALL fall back to the `(local)` prefix.

#### Scenario: Malformed rows name their registry

- **WHEN** a configured Git source reports a malformed resource, and another
  source reports a malformed resource with the same name
- **THEN** each row MUST be labelled with its own registry's display name
  rather than `(local)`, and the two rows MUST have distinct identities
- **AND** a malformed resource from a catalog with no configured source MUST
  still be labelled `(local)`
