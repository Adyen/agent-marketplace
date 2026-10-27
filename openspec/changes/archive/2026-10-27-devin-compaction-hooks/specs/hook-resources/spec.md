## ADDED Requirements

### Requirement: Devin compaction hooks use Devin's native compaction events

The Devin CLI hook catalog SHALL declare Devin's `PreCompact` and `PostCompact`
events with the `manual` and `auto` trigger matchers and the `command` handler
type. A canonical `PostCompact` hook SHALL compile to Devin `PostCompact`.
Devin's `PostCompaction` event SHALL remain accepted on ingestion and in
same-harness output but SHALL NOT be chosen for a hook from another harness.

#### Scenario: Canonical PreCompact compiles to Devin PreCompact

- **WHEN** a Claude Code `PreCompact` hook with matcher `auto` is compiled for
  Devin CLI
- **THEN** the artifact MUST declare the event `PreCompact`
- **AND** its matcher MUST select only the `auto` trigger
- **AND** no error diagnostic MUST be emitted

#### Scenario: Canonical PostCompact compiles to Devin PostCompact

- **WHEN** a Claude Code `PostCompact` hook is compiled for Devin CLI
- **THEN** the artifact MUST declare the event `PostCompact`
- **AND** it MUST NOT declare `PostCompaction`

#### Scenario: Devin PostCompaction keeps its spelling within Devin

- **WHEN** a hook ingested from a Devin `PostCompaction` entry is compiled for
  Devin CLI
- **THEN** the artifact MUST declare the event `PostCompaction`
- **AND** compiled for Claude Code it MUST declare `PostCompact`

#### Scenario: Devin compaction events ingest without a diagnostic

- **WHEN** a Devin hook file declares `PreCompact` and `PostCompact` entries
- **THEN** ingestion MUST emit no `ingest.devin.hook-event-unmappable`
  diagnostic for them
- **AND** they MUST resolve to the canonical `PreCompact` and `PostCompact`
  events
