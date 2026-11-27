## ADDED Requirements

### Requirement: Registry Git URL compatibility preserves a preceding suffix

Source configuration and v1/v2 acquisition-lock reads MUST remove exactly one terminal `.git` from a registry repository path and accept a canonical path that still ends in `.git`. Exact target-profile identity MUST continue to refuse an origin whose canonical registry path ends in `.git`.

#### Scenario: Registry inputs retain the preceding Git suffix

- **WHEN** source configuration or a v1/v2 acquisition lock uses `https://git.example/team/tools.git.git`
- **THEN** normalization MUST yield `https://git.example/team/tools.git`
- **AND** the lock MUST remain readable under its original API version

#### Scenario: Exact profile origins refuse repeated Git suffixes

- **WHEN** exact target-profile origin identity receives `https://git.example/team/tools.git.git`
- **THEN** identity validation MUST refuse the origin as unstable
