## ADDED Requirements

### Requirement: Portable manifest reads retain scoped ownership authority

Agent Marketplace MUST authorize portable manifest payloads below a captured repository/HOME scope before decoding ownership claims. The manifest leaf MUST be a direct regular singly-linked file; aliases and special or multiply linked leaves MUST refuse without being read as ownership payload. Existing contained parent and scope aliases MAY remain supported, while escaping parent aliases MUST refuse. Admitted bytes MUST describe one unchanged opened generation with captured-size streaming/growth checks; this adds no absolute byte or aggregate-work threshold. Absence remains valid. Refusal MUST report materialize.manifest.error and MUST NOT authorize a file-mode native output update from refused claims.

#### Scenario: Portable manifest alias refusal preserves native output

- **WHEN** an existing native file-mode skill update encounters a manifest leaf alias to foreign ownership data
- **THEN** materialization MUST report materialize.manifest.error, preserve installed native content, manifest alias and foreign bytes/identity/mode/mtime, and import no foreign ownership claims

#### Scenario: Contained portable manifest parent alias retains native updates

- **WHEN** a repository or machine portable manifest parent aliases a directory within its captured scope
- **THEN** actual native file-mode installation/update and read-only checking MUST remain valid, preserving the parent alias and existing ownership semantics

#### Scenario: Read-only portable manifest parent refusal is not absence

- **WHEN** a portable manifest parent aliases a missing directory and checking runs without mutation
- **THEN** materialization MUST report materialize.manifest.error rather than treating the unsafe parent as absent, preserve the alias and create no output
