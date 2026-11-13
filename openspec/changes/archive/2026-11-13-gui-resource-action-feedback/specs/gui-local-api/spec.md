## ADDED Requirements

### Requirement: Typed operation notification outcomes
Every local API notification payload SHALL include an outcome of success, warning or failure alongside its severity and message. The generated GUI client SHALL agree with the API contract. The GUI SHALL choose notification tone from the outcome, not message wording.

#### Scenario: Outcome drives notification tone
- **WHEN** success, warning and failure notification payloads reach the GUI with messages that do not end in " complete"
- **THEN** their tones SHALL be success, warning and danger respectively
- **AND** the API contract and generated client SHALL declare the same outcome values

### Requirement: Named operation completion messages
Operation notifications SHALL name the completed action or its failure without the word "mutation". Resource messages SHALL include the display name. Successful operations with warnings SHALL append " with N warning(s)" to their success message and carry the warning outcome. Failed operations SHALL carry the failure outcome even when warnings are present.

#### Scenario: Resource notifications name action and resource
- **WHEN** an install, remove, exclude or restore finishes successfully, fails, or succeeds with warnings
- **THEN** its message SHALL be "Installed <displayName>", "Removed <displayName>", "Excluded <displayName>" or "Restored <displayName>" on success, and "Couldn't install <displayName>", "Couldn't remove <displayName>", "Couldn't exclude <displayName>" or "Couldn't restore <displayName>" on failure
- **AND** success with warnings SHALL append " with N warning(s)"

#### Scenario: Configuration and lifecycle notifications name what changed
- **WHEN** adding a source, removing a source, another configuration change, Sync, Update or saving harness targets settles
- **THEN** success messages SHALL be "Source added", "Source removed", a name for the other changed setting, "Sync complete", "Update complete" or "Harness targets saved" respectively
- **AND** failures SHALL read "Couldn't add source", "Couldn't remove source", a named configuration failure, "Sync failed", "Update failed" or "Couldn't save harness targets" respectively
- **AND** no notification SHALL contain "mutation"
