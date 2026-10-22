## ADDED Requirements

### Requirement: Early target harnesses

Once the configuration layers validate, the GUI snapshot stream SHALL report the configured target harnesses with the configuration stage, without waiting for catalog acquisition or inventory resolution. Later stages SHALL report the same target set unless the configuration changed.

#### Scenario: Configuration stage reports target harnesses

- **WHEN** startup's catalog acquisition has not finished
- **THEN** the configuration-stage snapshot SHALL already list the configured target harnesses
