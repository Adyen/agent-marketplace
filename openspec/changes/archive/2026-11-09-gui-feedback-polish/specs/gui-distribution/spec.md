## ADDED Requirements

### Requirement: Native app name

On macOS the GUI's application name in the menu bar and its window title SHALL read "Agent Marketplace", both for the packaged `.app` and for the unbundled executable. The executable file name MAY remain `agent-marketplace-gui`, but that string SHALL NOT be the user-visible application name.

#### Scenario: Packaged app and unbundled binary are named Agent Marketplace

- **WHEN** the GUI runs on macOS from the packaged `.app` or as the unbundled executable
- **THEN** the application name the system reads for the menu bar MUST be "Agent Marketplace"
- **AND** the window title MUST be "Agent Marketplace"
