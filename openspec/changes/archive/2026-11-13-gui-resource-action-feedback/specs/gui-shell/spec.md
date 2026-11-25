## ADDED Requirements

### Requirement: Conditional resource confirmation
The GUI SHALL prepare every inspector resource action through its existing guarded plan flow. Install and Restore SHALL confirm a current plan immediately without a dialog when risk acceptance is not required. Remove and Exclude SHALL always open the dialog. A plan preparation failure SHALL open the dialog with its error and Retry plan. The installed resource SHALL retain the existing status-group selection behavior.

#### Scenario: Plain install and restore skip the dialog
- **WHEN** the user activates Install or Restore and its current plan needs no risk acceptance
- **THEN** the GUI SHALL confirm the plan without opening a dialog and write the selection through the existing action endpoint
- **AND** the action button SHALL show a busy state with a progress cursor during planning and submission

#### Scenario: Risk and preparation failure keep the dialog
- **WHEN** an Install plan requires Marketplace risk acceptance or preparing an Install or Restore plan fails
- **THEN** the GUI SHALL open the dialog with the unchecked risk acknowledgement or the error and Retry plan respectively
- **AND** no action SHALL be submitted before risk acceptance or a successful retry

#### Scenario: Remove and exclude keep confirmation
- **WHEN** the user activates Remove or Exclude
- **THEN** the GUI SHALL open its confirmation dialog and SHALL NOT submit the action until confirmation

### Requirement: Shared modal header alignment
Every non-bare GUI modal SHALL put its title and close button on the same row with the same vertical center in light and dark themes.

#### Scenario: Modal title and close align in both themes
- **WHEN** a non-bare modal is displayed in light or dark theme
- **THEN** its title and close button SHALL share a row and vertical center

### Requirement: Plain-language GUI progress
GUI progress toast titles SHALL end with an ellipsis and name the running action. Resource titles SHALL name the resource display name. Progress detail SHALL show only an observed phase, never the generic "Working…". Resource planning and submission SHALL read "Checking <displayName>…" and the action's progressive verb followed by the display name and an ellipsis. Notifications, progress toasts and resource dialog text SHALL NOT contain "mutation" or "resource action".

#### Scenario: Progress names the action without invented phases
- **WHEN** an operation runs with no observed phase and then reports a real phase
- **THEN** its progress title SHALL end with "…", naming the resource for a resource action, and its detail SHALL be absent until the real phase is reported
- **AND** resource planning and submission SHALL use the named Checking and progressive-verb messages, without "mutation", "resource action" or "Working…"

## MODIFIED Requirements

### Requirement: Resource action progress spinner
The resource action dialog SHALL show a spinner and a progress cursor while its plan is being prepared or its confirmed action is in flight, and SHALL hide the spinner when the request succeeds or fails. Disabled buttons SHALL NOT use a pointer cursor.

#### Scenario: Spinner shows only while the action is in flight
- **WHEN** the user confirms an install and the request is in flight, and the request then succeeds or fails
- **THEN** the dialog MUST show a spinner and a progress cursor while the request is in flight
- **AND** the spinner MUST be hidden after the request succeeds or fails
- **AND** disabled buttons MUST NOT have a pointer cursor
