## ADDED Requirements

### Requirement: Job-mode coverage follows indexed skill revisions

In Job mode, Marketplace MUST automatically account for each enabled analyzer for every indexed skill revision, regardless of registry ownership or acquisition mode. Refresh deadlines and restarts MUST NOT silently discard outstanding work. Unavailable or unsupported input MUST yield an explicit non-success outcome. Supplementary work MUST NOT delay otherwise valid publication or change the built-in decision.

#### Scenario: Third-party registry supplies no scan report

- **WHEN** Marketplace indexes a skill from an index-only registry without CI or publisher reports
- **THEN** Marketplace MUST independently request analysis of that revision and expose only its own validated evidence
- **AND** that scan MUST NOT make an installable Marketplace archive available

#### Scenario: Outstanding work survives the registry deadline

- **WHEN** a refresh with several skills ends before the last scan starts and the dispatcher restarts
- **THEN** each outstanding revision MUST remain eligible for an attempt without waiting for a new source change
- **AND** a backed-off or exhausted attempt MUST NOT appear as a completed scan with zero findings

### Requirement: Analyzer results have independent identities

Marketplace MUST associate each result with the exact skill input and operator-selected analyzer configuration used for the attempt. It MUST preserve independently attributed results and immutable past assessments. A completed result for unchanged input and configuration MUST be reusable without rerunning the tool.

#### Scenario: Late result cannot qualify a newer revision

- **WHEN** a result for revision A arrives after revision B becomes current at the same coordinate
- **THEN** a reader of B MUST NOT see A's result as evidence for B
- **AND** fetching a previously issued assessment by digest MUST return the original evidence

#### Scenario: Changing one analyzer does not invalidate another

- **WHEN** two enabled analyzers have completed for a revision and the operator changes only one analyzer's image or configuration
- **THEN** only the changed analyzer MUST require new work for that revision
- **AND** repeated refresh or result delivery MUST NOT duplicate the other analyzer's findings
