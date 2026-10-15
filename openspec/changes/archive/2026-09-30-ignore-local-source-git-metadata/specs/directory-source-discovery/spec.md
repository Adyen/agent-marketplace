## Purpose

Defines how Agent Marketplace discovers authored resources from directory sources while excluding Git metadata from resource candidates and diagnostics.

## ADDED Requirements

### Requirement: Git metadata is not a skill candidate
When ingesting a directory source, Agent Marketplace SHALL ignore a `.git` directory beneath a source layout's `skills/` directory as a skill candidate. It SHALL continue to discover authored skills and report malformed authored skills without reporting Git metadata as malformed or as an ignored sub-bundle.

#### Scenario: Git metadata beside valid and malformed skills
- **WHEN** a v4 directory source contains `skills/.git/` with Git metadata, an authored valid skill, and an authored malformed skill
- **THEN** listing the source and opening it in the TUI show the valid skill and the malformed authored skill, and do not report `skills/.git` as a malformed resource or an ignored sub-bundle
