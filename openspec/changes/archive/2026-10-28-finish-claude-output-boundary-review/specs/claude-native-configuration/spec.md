## ADDED Requirements

### Requirement: Relocated cleanup authority does not change legacy eligibility

The captured-file eligibility checks for declared relocated Claude aggregates
SHALL NOT make legacy recursive cleanup reject unrelated unmarked JSON that it
previously ignored. Synchronization and read-only checks MUST preserve those
files while still processing eligible owned sibling entries.

#### Scenario: Legacy cleanup preserves unmarked hard-linked JSON

- **WHEN** a legacy cleanup root contains unmarked hard-linked JSON and a stale owned sibling
- **THEN** synchronization and read-only checking MUST NOT report an error for the unmarked files or change their bytes or metadata
- **AND** synchronization MUST prune the stale owned sibling while read-only checking reports its drift without writing

### Requirement: Relocated Claude roots exclude Git administration

A relocated Claude root SHALL NOT be at or inside a literal `.git` path, a
configured `GIT_DIR` or `GIT_COMMON_DIR`, or the repository's resolved Git or
linked-worktree common metadata. This restriction includes physical aliases and
prospective missing descendants. Refusal MUST precede Claude writes, pruning and
root creation, while independent selected harness targets continue. An otherwise
eligible ancestor containing metadata SHALL retain its eligibility.

#### Scenario: Git administrative roots refuse

- **WHEN** the configured Claude root is a literal Git path, a configured Git directory, a common or linked-worktree metadata path, or a physical alias into those locations
- **THEN** destination admission MUST refuse before creating or changing Claude output there
- **AND** independent harness selections MUST continue normally

#### Scenario: Nonadministrative roots remain eligible

- **WHEN** the relocated root is disjoint from Git metadata or is an otherwise eligible ancestor containing it
- **THEN** destination admission MUST retain the existing eligibility of that root
