## MODIFIED Requirements

### Requirement: OpenCode detection uses a source marker

Autodetection SHALL recognize an OpenCode source marker only when `.opencode/` holds an OpenCode resource location (`skills`, `commands`, `agents`, `plugins`, their singular aliases, or `hooks/agent-marketplace`) or an `opencode.json`/`opencode.jsonc` file; other content such as dependency manifests SHALL NOT be a marker. Autodetection SHALL NOT infer OpenCode from shared skills directories alone. When a source without a plugin manifest or Devin marker would be detected without OpenCode as a layout that carries its own workspace marker (`.claude`, `.codex`, `.factory`, `.cursor`, `.gemini`, or `.windsurf`), that layout SHALL keep precedence over an OpenCode marker. Otherwise an OpenCode marker SHALL win over shared skills locations, except that a source root with no other detected layout that contains bundle subdirectories SHALL remain a container of those bundles. Container discovery SHALL skip `.opencode` as a harness folder. Whenever an OpenCode marker coexists with another detected layout, autodetection SHALL report warning `ingest.autodetect.ambiguous` naming the selected and ignored layouts and recommending an explicit `source_harness`. Explicit source-layout selection SHALL remain available and SHALL NOT report the warning. Supported native plural directories and legacy singular aliases SHALL be ingested with documented deterministic precedence; duplicate identities without an unambiguous native winner SHALL be suppressed with severity-error diagnostic `ingest.opencode.duplicate-identity`.

#### Scenario: OpenCode marker distinguishes a shared skills layout

- **WHEN** a bundle contains an `.opencode` source marker alongside shared `.agents/skills` or bare skills locations
- **THEN** autodetection SHALL identify OpenCode, discover its supported resources, and report `ingest.autodetect.ambiguous`
- **AND** removing all OpenCode-specific evidence SHALL prevent shared skills locations alone from identifying OpenCode

#### Scenario: Existing workspace layouts keep precedence over an OpenCode marker

- **WHEN** a source without a plugin manifest carries Claude, Codex, Factory, Cursor, Gemini, or Windsurf workspace resources and an `.opencode` source marker
- **THEN** autodetection SHALL select the layout it selected before OpenCode was registered and ingest its resources

#### Scenario: A dependency-only OpenCode directory is not a marker

- **WHEN** `.opencode/` contains only dependency manifests or installed dependencies
- **THEN** autodetection SHALL NOT identify OpenCode and SHALL NOT report a coexisting layout

#### Scenario: Coexisting layouts are reported

- **WHEN** autodetection selects a layout while another detected layout coexists with an OpenCode marker
- **THEN** the catalog SHALL carry warning `ingest.autodetect.ambiguous` naming both layouts and recommending `source_harness`
- **AND** an explicit `source_harness` SHALL select that layout without the warning

#### Scenario: Container registries keep their bundles beside an OpenCode folder

- **WHEN** a source root without another detected layout contains bundle subdirectories and an `.opencode` source marker
- **THEN** discovery SHALL read the bundles, not read `.opencode` as a bundle, and report `ingest.autodetect.ambiguous`
- **AND** an explicit `source_harness: opencode` SHALL read the root as OpenCode

#### Scenario: Native aliases do not create duplicate resources

- **WHEN** supported plural and legacy singular directories or native declarations describe the same resource identity
- **THEN** ingestion SHALL apply the documented native precedence or report `ingest.opencode.duplicate-identity` and suppress the ambiguous identity
- **AND** independent valid resources SHALL remain discoverable
