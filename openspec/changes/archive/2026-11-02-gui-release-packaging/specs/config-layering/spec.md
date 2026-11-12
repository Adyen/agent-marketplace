## REMOVED Requirements

### Requirement: Organization-only GUI release check setting

**Reason**: The GUI follows the CLI and TUI release-check setting `update_check.enabled` and suppression rules under `upgrade-notice`, so one setting governs every surface.

**Migration**: Replace `gui.version_check` with `update_check.enabled` in organization or user-personal configuration. The setting never shipped in a release; a configuration that still declares `gui` is rejected as an unknown field.
