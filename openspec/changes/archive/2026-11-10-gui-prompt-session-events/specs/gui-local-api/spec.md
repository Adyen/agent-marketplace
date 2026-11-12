## ADDED Requirements

### Requirement: Prompt session event delivery in the desktop window

The desktop window SHALL receive each session event as soon as the local API emits it, without waiting for a later request, user action, or event. The window's event stream SHALL carry the same ordered events, generation, and sequence identity as the HTTP event stream, and SHALL require the per-launch credential.

#### Scenario: Completion notification renders without further activity

- **WHEN** a Sync or Update settles in the desktop app and no further request, user action, or event follows
- **THEN** its final snapshot and completion notification MUST reach the window, and the notification MUST render without further activity

#### Scenario: Window event stream requires the credential

- **WHEN** the window opens a session event stream without the per-launch credential
- **THEN** no session event MUST be delivered on that stream, and the window MUST receive an unauthorized refusal
