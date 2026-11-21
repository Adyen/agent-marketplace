## ADDED Requirements

### Requirement: Consistent Marketplace registry label

Every Marketplace resource row SHALL carry the registry label `<endpoint>/<registry>`: the endpoint alias (or normalized URL without one) and the hosted registry slug, whether installed, pending verification, or discovered. Without a known slug the label SHALL be `<endpoint>`. The label SHALL NOT change row identity, source association, or source grouping.

#### Scenario: Installed and available Marketplace rows share a registry label

- **WHEN** one resource from a Marketplace registry is installed, another is pending verification, and a third is only available from discovery
- **THEN** all three rows MUST carry the same `<endpoint>/<registry>` registry label
- **AND** their row identities and source associations MUST be unchanged

## MODIFIED Requirements

### Requirement: Named operation completion messages
Operation notifications SHALL name the completed action or its failure without the word "mutation". Resource messages SHALL include the display name. Successful operations with warnings SHALL append " with N warning(s)" to their success message and carry the warning outcome. Failed operations SHALL carry the failure outcome even when warnings are present.

#### Scenario: Resource notifications name action and resource
- **WHEN** an install, uninstall, exclude or restore finishes successfully, fails, or succeeds with warnings
- **THEN** its message SHALL be "Installed <displayName>", "Uninstalled <displayName>", "Excluded <displayName>" or "Restored <displayName>" on success, and "Couldn't install <displayName>", "Couldn't uninstall <displayName>", "Couldn't exclude <displayName>" or "Couldn't restore <displayName>" on failure
- **AND** success with warnings SHALL append " with N warning(s)"

#### Scenario: Configuration and lifecycle notifications name what changed
- **WHEN** adding a source, removing a source, another configuration change, Sync, Update or saving harness targets settles
- **THEN** success messages SHALL be "Source added", "Source removed", a name for the other changed setting, "Sync complete", "Update complete" or "Harness targets saved" respectively
- **AND** failures SHALL read "Couldn't add source", "Couldn't remove source", a named configuration failure, "Sync failed", "Update failed" or "Couldn't save harness targets" respectively
- **AND** no notification SHALL contain "mutation"
