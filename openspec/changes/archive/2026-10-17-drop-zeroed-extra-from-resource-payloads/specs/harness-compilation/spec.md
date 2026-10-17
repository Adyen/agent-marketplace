## ADDED Requirements

### Requirement: Newly encoded resource payloads carry no zeroed Extra member

The canonical encoder MUST NOT write the IR's zeroed `Extra` field, nor an extension field named exactly `Extra` whose value is null, which is how a resource decoded from an earlier payload carries that member. Authored extension fields, including one named `Extra` with a non-null value, MUST still be encoded. Decoding is unchanged, so payloads published earlier keep their existing output, as the Marketplace API requirement for non-hook canonical metadata demands.

#### Scenario: Decoded resources compile without a spurious Extra field

- **WHEN** a skill, a command, a subagent and a hook are encoded to resource payloads, decoded, and compiled to Claude
- **THEN** no payload MUST contain an `Extra` member, and no compiled artifact MUST contain an `Extra` field

#### Scenario: Re-encoding an earlier payload drops the null Extra member

- **WHEN** a hook payload published before this rule, containing `"Extra": null`, is decoded and re-encoded, the same hook is encoded directly, and a skill with an authored `Extra` extension field holding a value is encoded, decoded and compiled to Claude
- **THEN** both hook payloads MUST be byte-identical without an `Extra` member, and the authored field MUST be kept with its value
