# Proposal

## Why

Desktop GUI users reported functional defects. Adding a directory source whose path does not exist leaves the workspace unusable. Diagnostics shows Marketplace discovery health filtered by the resource search. Users also reported that Enter and the Open resource button behave inconsistently across source kinds.

## What Changes

- A directory source whose path does not exist is refused inside the Add source section without writing configuration, and the workspace stays usable.
- Diagnostics shows Marketplace discovery health independent of the current resource search.
- Enter and the Open resource button keep one availability rule across Marketplace, directory, and Git skill rows.
- Arrow, Home, and End keys move focus between Settings tabs without an error.

## Capabilities

### New Capabilities

### Modified Capabilities

- `gui-configuration`: adding an unusable directory source fails recoverably.
- `gui-shell`: Enter and Open resource agree; Diagnostics discovery health does not depend on the resource search.

## Impact

Desktop GUI frontend (`gui/src`) and the GUI local API (`internal/localapi`). No CLI or TUI behavior changes.

## Release note

The desktop app now keeps working after an invalid directory source is entered, and Diagnostics shows Marketplace discovery health regardless of the search box.
