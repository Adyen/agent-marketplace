## ADDED Requirements

### Requirement: Prerequisite remediation arguments are shell literals

Agent Marketplace MUST format registry-authored prerequisite resource names, registry locations and paths as literal POSIX shell arguments in install remediation. Command/variable substitution, whitespace, delimiters and embedded single quotes MUST NOT gain shell meaning from display quoting. Existing credential redaction MUST occur before argument formatting. Typed unmet warnings, resource selection and requested kind/location semantics MUST remain unchanged. This requirement changes human-readable command guidance, not resource schemas or machine-readable warning identities.

#### Scenario: Requirement remediation keeps shell syntax literal

- **WHEN** prerequisite name or location arguments contain inert shell syntax or embedded quotes
- **THEN** install remediation MUST represent them as literal arguments using the established POSIX formatter after credential redaction

#### Scenario: Resolver keeps ordinary prerequisite warnings and selection

- **WHEN** an otherwise selected resource declares an unmet prerequisite
- **THEN** resolution MUST keep the selected resource and typed warning while its install hint represents the prerequisite name literally
