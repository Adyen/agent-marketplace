## ADDED Requirements

### Requirement: Marketplace requires edges are read and canonicalized

An acquisition-lock Marketplace resource `selected_by` entry SHALL accept an optional `kind`. An entry without `kind` SHALL remain a collection edge with unchanged validation and encoding, so locks without requires edges re-encode byte-identically. An entry whose `kind` is a non-collection Marketplace resource kind SHALL be a requires edge. It names the requiring locked resource by registry ID, kind, and name, and its `revision` equals that resource's content digest. Canonical ordering and duplicate removal SHALL include `kind`. The lock MUST be rejected as an invalid document when an entry has an explicit `collection` kind or a kind that is not a Marketplace resource kind, names no locked resource, carries a mismatched digest, names its own resource, or names an unbound registry.

#### Scenario: Collection-only lock re-encodes byte-identically

- **WHEN** a lock whose `selected_by` entries carry no `kind` is read and encoded
- **THEN** the encoded bytes MUST equal the canonical input

#### Scenario: Requires edge round-trips canonically

- **WHEN** a lock contains a requires edge naming a locked requiring resource with its content digest
- **THEN** reading and encoding MUST preserve the edge with its `kind` in canonical order

#### Scenario: Malformed requires edges are rejected

- **WHEN** a requires edge has an explicit collection kind, an unknown kind, a missing parent, a mismatched digest, a self reference, or an unbound registry
- **THEN** reading the lock MUST fail as an invalid document
