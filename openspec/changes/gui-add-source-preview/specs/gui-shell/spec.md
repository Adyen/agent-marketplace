## MODIFIED Requirements

### Requirement: Add source keyboard submission and cancellation

The Add source dialog SHALL allow Enter in a text input to submit a valid form. Enter with invalid input SHALL NOT submit. Submitting a Git or directory source SHALL request its preview; submitting a Marketplace source SHALL send the add request. The Cancel button SHALL close the dialog without submitting and SHALL be unavailable while an add is pending.

#### Scenario: Cancel and Enter in Add source

- **WHEN** the user selects Cancel, or presses Enter in a text input while the form is invalid
- **THEN** no preview or add request MUST be sent
- **AND** Cancel MUST close the dialog
- **WHEN** the user presses Enter in a text input with a valid directory source
- **THEN** the preview MUST be requested and shown without sending an add request
- **AND** confirming the preview MUST send the add request

## ADDED Requirements

### Requirement: Add source preview step

The Add source dialog SHALL show a preview of a Git or directory source before adding it: the number of resources, the ref and commit that were read for a Git source, and each resource with its kind, name, and description. A resource that cannot be read SHALL be listed with its problem. The preview SHALL offer Back, which returns to the form with its values kept, and Add source, which adds a Git source at the previewed ref. A source that cannot be loaded SHALL keep the form, with a plain-language summary and the original error text available under Details.

#### Scenario: Add source previews a Git source before adding

- **WHEN** the user previews a Git source without a ref
- **THEN** the dialog MUST show the resource count, the short commit, the resolved branch, and the resources found
- **AND** a resource that cannot be read MUST be listed with its problem and excluded from the count
- **AND** Add source MUST send the add request with the resolved branch as the ref

#### Scenario: Add source preview failure stays on the form

- **WHEN** the previewed source cannot be loaded
- **THEN** the form MUST stay with its values and no preview MUST be shown
- **AND** the dialog MUST show a plain-language summary with the original error text under Details
- **AND** no add request MUST be sent

### Requirement: Add source preview installs picked resources

Each readable resource in the Add source preview SHALL have a switch, off by default, and the preview SHALL offer one switch that turns all of them on or off. A resource that cannot be read SHALL have no switch. The confirm button SHALL read "Add source" when nothing is turned on and "Add source and install N" otherwise, and SHALL send the turned-on resources and the previewed commit with the add request.

#### Scenario: Add source preview installs the picked resources

- **WHEN** the user turns on resources in the preview and confirms
- **THEN** the add request MUST carry those resources and the previewed commit
- **AND** a resource that cannot be read MUST NOT offer a switch
- **AND** the confirm button MUST name how many resources will be installed
