## ADDED Requirements

### Requirement: Transient organization acquisition accepts staged configuration edits

Transient organization Marketplace acquisition SHALL NOT treat a configuration layer supplied as a staged edit as changed on disk. The enclosing transaction validates staged edits against their own expected generations.

#### Scenario: Staged user configuration does not conflict with transient acquisition

- **WHEN** an online load reads a staged user configuration edit while the organization configuration needs a transient Marketplace lock
- **THEN** the load MUST succeed without a generation conflict
