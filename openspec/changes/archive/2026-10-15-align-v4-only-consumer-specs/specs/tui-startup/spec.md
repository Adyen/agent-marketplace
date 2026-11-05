## MODIFIED Requirements

### Requirement: Progressive staged snapshot delivery

The TUI SHALL validate required consumer roots and include trees under `consumer-config-format` before delivering usable startup configuration or rows. For valid input, it SHALL deliver the startup snapshot progressively under one request generation: a provisional stage of configured selections, a config stage publishing the validated merged configuration, an inventory stage of verified rows with drift pending, and a final assessed stage. Unsupported input MUST produce the path-specific format refusal without legacy operation or in-process conversion.

#### Scenario: Startup snapshot delivers config, inventory, then assessed stages

- **WHEN** a startup snapshot resolves a valid v4 merged configuration
- **THEN** it MUST deliver, in order under one shared generation, a provisional config stage whose rows are all marked verification-pending, a config stage publishing the validated merged configuration without rows, an inventory stage publishing verified rows with drift still pending, and a final stage publishing assessed drift and diagnostics

#### Scenario: Invalid configuration delivers one fail-closed final stage

- **WHEN** a required root or include tree cannot be parsed or validated during startup, including an unsupported root or explicit fragment version
- **THEN** the snapshot MUST deliver exactly one final stage carrying diagnostics
- **AND** no provisional, config, or inventory stage MUST be delivered
- **AND** unsupported consumer input MUST retain the path-specific diagnostic from `consumer-config-format`

#### Scenario: Config stage starts cached-first Marketplace discovery before local verification finishes

- **WHEN** the config stage publishes the validated merged configuration and that configuration declares Marketplace endpoints
- **THEN** cached-first Marketplace discovery MUST start immediately without waiting for local catalog verification
- **AND** cached discovery rows delivered during local verification MUST render without advancing the snapshot generation
