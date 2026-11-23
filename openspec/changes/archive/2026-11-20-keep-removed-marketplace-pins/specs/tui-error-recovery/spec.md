## ADDED Requirements

### Requirement: TUI update reports kept removed Marketplace resources

When an update started from the TUI keeps a Marketplace pin removed upstream
because its pinned revision is usable, the update SHALL complete as a success
with warnings and SHALL list the CLI's removed-resource warning in the
diagnostics view. When the pinned revision is not usable, the TUI update SHALL
fail with the CLI's missing-resource error.

#### Scenario: TUI update shows the removed-resource warning

- **WHEN** a workspace has a synced Marketplace pin whose resource was removed upstream with its exact revision still served, and the user starts an update from the TUI
- **THEN** the completion toast MUST read `Update complete with 1 warning(s)` rather than `Update complete`
- **AND** the diagnostics view MUST list a warning naming the resource and the `agent-marketplace remove <installed name>` remedy
- **AND** the acquisition lock MUST be byte-identical afterwards

#### Scenario: TUI update fails when the pinned revision is unusable

- **WHEN** the removed pin's exact revision is no longer served and is not in the verified cache, and the user starts an update from the TUI
- **THEN** the TUI update MUST fail, and the errors screen MUST name the resource with the remove-and-install hint
- **AND** the diagnostics screen MUST also show warnings produced while preparing acquisition locks for the failed update
