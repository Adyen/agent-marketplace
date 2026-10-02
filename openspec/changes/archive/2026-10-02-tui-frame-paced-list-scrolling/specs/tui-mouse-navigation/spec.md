## ADDED Requirements

### Requirement: Resource-list wheel bursts preserve ordered selection

The TUI SHALL apply every accepted vertical resource-list wheel event in its original order, including events at a list boundary. Frame pacing SHALL NOT change scroll distance, modifier handling, ignored mouse events, or selection through wrapped resource rows.

#### Scenario: Rapid wheel reversals preserve selection

- **WHEN** a burst of vertical wheel events changes direction at the first or last list entry or traverses wrapped resource rows
- **THEN** the final selection MUST equal applying every event in order under the existing one-entry navigation rules
- **AND** ignored mouse events and Shift-modified vertical input MUST retain their existing behavior

### Requirement: Resource-list wheel presentation is frame-paced

During a resource-list wheel-only burst, the TUI SHALL refresh the resource-list presentation at most once per 60 Hz frame interval while applying navigation input without waiting for that refresh. A pending final presentation update SHALL run after the last event without requiring more user input.

#### Scenario: Final selection appears after a wheel burst

- **WHEN** several resource-list wheel events arrive within one frame interval and then input stops
- **THEN** the TUI MUST present the newest selected resource after the pending frame update
- **AND** it MUST NOT construct a separate presentation for each intermediate selection

### Requirement: Other input and state changes bypass wheel presentation pacing

Keyboard input, terminal resize, asynchronous state updates, and pane changes SHALL refresh from the current TUI state immediately rather than waiting behind a resource-list wheel presentation timer. Pending presentation timers SHALL NOT restore an obsolete pane or selection, and commands produced by state updates SHALL remain executable.

#### Scenario: Non-wheel updates supersede pending frames

- **WHEN** a key, resize, snapshot, or pane change follows resource-list wheel input before its pending frame update
- **THEN** that update MUST use the current selection and display the resulting state immediately
- **AND** a stale presentation timer MUST NOT revert that state or suppress the update's commands

### Requirement: Wheel bursts retain their complete terminal outcome

Resource-list presentation pacing SHALL preserve the complete input outcome through the normal terminal input path. Verification of wheel bursts SHALL assert an independently expected final selection and retain all accepted events rather than achieving lower latency by discarding input.

#### Scenario: Terminal wheel burst reaches the expected resource

- **WHEN** the active resource list receives a bounded burst of encoded vertical wheel input followed by a direction reversal
- **THEN** the terminal MUST show the independently expected selected resource and its details
- **AND** the completed burst MUST preserve the accepted event count and order
