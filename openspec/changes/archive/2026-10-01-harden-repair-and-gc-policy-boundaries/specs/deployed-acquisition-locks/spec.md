## ADDED Requirements

### Requirement: Organization acquisition errors preserve independent diagnostics

An organization acquisition failure SHALL replace the stale organization missing-evidence diagnostic without discarding errors from independent configuration layers.

#### Scenario: Independent layer errors survive organization acquisition failure

- **WHEN** organization acquisition fails and another configuration layer has an independent error
- **THEN** both the actual organization acquisition failure and the independent error SHALL remain available to the consumer
