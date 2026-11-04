## ADDED Requirements

### Requirement: Prompt session event delivery

The GUI SHALL receive each session event as soon as the local API emits it, including the completion notification that follows an operation's final snapshot. A notification SHALL NOT wait for a later event, request, or user action before the GUI renders it.

#### Scenario: Completion notification renders without further action

- **WHEN** an Update or Sync operation settles and no further session event or user action follows
- **THEN** its completion notification MUST render in the GUI
