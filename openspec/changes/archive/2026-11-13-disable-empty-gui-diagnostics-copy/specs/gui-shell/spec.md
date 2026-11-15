# Spec Delta

## ADDED Requirements

### Requirement: Diagnostic copying

The GUI SHALL disable Copy diagnostics when there are no diagnostic messages, and activating the empty-state control SHALL NOT write to the clipboard or show successful-copy feedback.

#### Scenario: Empty diagnostics cannot be copied

- **WHEN** the Diagnostics view contains no messages
- **THEN** Copy diagnostics is disabled
- **AND** clipboard contents remain unchanged and no Copied feedback is shown
