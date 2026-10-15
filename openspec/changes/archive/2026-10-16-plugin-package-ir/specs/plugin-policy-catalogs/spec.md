## ADDED Requirements

### Requirement: Conversion fixtures prove plugin catalog policies

The catalog check SHALL run hand-written plugin conversion fixtures through
the public package compilation path, and each fixture's expected projection
and diagnostics MUST match.

#### Scenario: A plugin fixture mismatch fails the check

- **WHEN** a plugin catalog edit changes the projection of a member covered
  by a conversion fixture
- **THEN** the catalog check MUST fail and report the fixture
