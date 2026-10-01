## ADDED Requirements

### Requirement: Shared Gemini native work does not introduce ordinary pruning

When shared Gemini native settings reconciliation adds machine-scope work without independently compiled or blocked machine resources, Agent Marketplace SHALL reconcile the shared native settings without running an additional ordinary artifact or skill pruning pass. Repository commands MUST remain selected through synchronization and check-only inspection in both materialization modes, including filesystem aliases of HOME. An empty desired set SHALL retain ordinary stale-output cleanup.

#### Scenario: Shared Gemini native work preserves repository commands

- **WHEN** repository commands are selected at a materialization root that identifies HOME, with or without repository native settings fragments
- **THEN** first and repeated synchronization MUST preserve the commands in both materialization modes
- **AND** check-only inspection MUST report no drift and leave their bytes and modification times unchanged
- **AND** stale repository commands MUST still be pruned when an active repository selection no longer desires them

#### Scenario: Empty shared Gemini desired set retains cleanup

- **WHEN** the last desired repository command and native settings entries are removed from a shared Gemini root in symlink materialization mode
- **THEN** synchronization MUST remove the stale command and owned native settings entries
- **AND** it MUST preserve handwritten settings
