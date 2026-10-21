## ADDED Requirements

### Requirement: Landing motion remains accessible and resilient
Landing SHALL animate the hero once on page entry and decode section eyebrow
labels once when they first become visible. Real content MUST remain in the DOM
and accessible throughout; decorative duplicate or noise text MUST be hidden
from assistive technology. A live reduced-motion preference SHALL show final
content without arrival, label-roll, decode, or travelling-indicator movement
while preserving copy, selection, and keyboard functionality.

#### Scenario: Reduced motion preserves final Landing content and controls
- **WHEN** a user visits Landing with reduced motion enabled
- **THEN** hero and eyebrow content is immediately visible without displacement or decoding noise
- **AND** action, copy, and tab controls remain functional without movement

### Requirement: Command copying reports truthful feedback
After a Landing command is successfully copied, its control SHALL show
`Copied`, use the success fill, announce success politely, and restore `Copy`
after the feedback interval. A rejected clipboard write MUST NOT show success.
The copied command bytes MUST remain unchanged, and pending feedback work MUST
be cancelled when the component unmounts.

#### Scenario: Successful command copy is visible and announced
- **WHEN** a user activates Copy and the clipboard write succeeds
- **THEN** the control shows `Copied` with success presentation and a polite success announcement
- **AND** the clipboard receives the exact displayed command

### Requirement: Harness tabs provide one accessible selection model
Landing harness tabs SHALL expose tab and tabpanel relationships, one roving
tab stop, and automatic selection with wrapping Left, Right, Up, and Down arrow
navigation plus Home and End. A single decorative indicator MAY follow the
selected tab but MUST NOT create a second selection model.

#### Scenario: Harness tabs wrap keyboard selection
- **WHEN** a user navigates the harness tabs with arrows, Home, or End
- **THEN** focus and selection move together according to the wrapping tab order
- **AND** exactly one tab remains selected and keyboard-focusable
