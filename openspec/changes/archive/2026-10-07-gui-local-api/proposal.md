## Why

A desktop GUI needs a local boundary for reading session state, receiving progress, and requesting actions without exposing an unauthenticated control surface to other local processes or web origins. A checked contract and shared mutation gate let the GUI use the same session guarantees as the TUI.

## What Changes

- Define a credentialed, origin-restricted local API for the GUI and test harness.
- Stream ordered snapshot, progress, and notification events with generation and sequence identity.
- Route all mutating requests through the shared guarded operation and reject overlapping work as busy.
- Check in an OpenAPI contract, generate the GUI client from it, and fail the drift check when the client diverges.

## Capabilities

### New Capabilities
- `gui-local-api`: Credentialed and origin-restricted local access, the published API contract, ordered session events, and single-writer mutations.

### Modified Capabilities

## Impact

The API boundary connects the extracted interactive-session core to the desktop GUI and its test harness. It adds a checked-in OpenAPI document, generated client artifacts, and API security, event-stream, mutation, and drift tests. The production transport choice remains subject to the streaming compatibility spike described in design.md.
