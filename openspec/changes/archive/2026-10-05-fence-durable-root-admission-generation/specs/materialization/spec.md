## ADDED Requirements

### Requirement: Native durable reconciliation refuses ordinary root replacement after admission
Agent Marketplace native durable aggregate reconciliation MUST refuse an ordinary directory replacement of a previously existing admitted target root after lock acquisition, even when the replacement aggregate has identical desired bytes. It MUST report the existing materialization write-error diagnostic rather than certify a successful no-op, preserve foreign and retained aggregate bytes and metadata, and retain valid fresh-root and restored-original-root materialization.

#### Scenario: Gemini copied replacement root refuses after lock acquisition
- **GIVEN** a real native Gemini MCP aggregate was materialized and its root admitted for a subsequent Run
- **WHEN** actual ownership lock acquisition replaces the root with an ordinary foreign directory containing identical settings
- **THEN** Run MUST report materialization write refusal, preserve both aggregate generations, and allow actual materialization after restoration of the original root
