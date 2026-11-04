# Proposal

## Why

Desktop GUI users reported functional defects: Update and Sync completion notifications appear only after a later action, adding a directory source whose path does not exist leaves the workspace unusable, Enter and the Open resource button disagree for directory and Git skills, and Diagnostics shows Marketplace discovery health filtered by the resource search. Some installs also stop on a generic "could not be prepared" message or leave a different post-install state depending on source kind.

## What Changes

- Session events, including the completion notification that follows an operation's final snapshot, reach the GUI as soon as they are emitted rather than waiting for later traffic.
- A directory source whose path does not exist is refused inside the Add source section without writing configuration, and the workspace stays usable.
- Enter and the Open resource button offer and open the inspector for the same rows across Marketplace, directory, and Git skills.
- Diagnostics shows Marketplace discovery health independent of the current resource search.
- The "Resource action could not be prepared" install failure and the inconsistent post-install state are root-caused and fixed, or recorded for a follow-up decision when a product choice is required.

## Capabilities

### New Capabilities

### Modified Capabilities

- `gui-local-api`: session events are delivered promptly.
- `gui-configuration`: adding an unusable directory source fails recoverably.
- `gui-shell`: Enter and Open resource agree; Diagnostics discovery health does not depend on the resource search.

## Impact

Desktop GUI frontend (`gui/src`), the GUI local API (`internal/localapi`), and the GUI application host (`cmd/agent-marketplace-gui`). No CLI or TUI behavior changes are intended.

## Release note

The desktop app now shows Update and Sync completion right away, keeps working after an invalid directory source is entered, opens directory and Git skills with Enter or the Open button consistently, and shows Marketplace discovery health in Diagnostics regardless of the search box.
