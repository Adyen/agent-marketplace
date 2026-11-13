# Design

## Context
The controller already validates plan identity, generation, action and risk acknowledgement before submitting. The local API serializes writes and emits final snapshots before notifications. See proposal.md for motivation.

## Goals / Non-Goals
Preserve guarded writes, authoritative settlement, stale-event isolation and existing selection behavior. Do not add an install auto-sync or a second write path.

## Decisions
- Keep planning state separate from dialog visibility. Auto-confirm only a current risk-free Install or Restore plan through the existing confirmation method. Errors reveal the dialog. Remove, Exclude and risk plans stay explicit.
- Carry a private operation subject to the local API runner so completion copy names what changed. Keep operation kind and identity unchanged. Add the notification outcome to the public payload and regenerate the client instead of parsing message text.
- Retain the resource action identity and display name through authoritative settlement for progress copy. Do not change selected rows when status grouping moves them.
- Align the shared modal header and apply busy cursor styling at the dialog frame. Disabled controls must not override it with a pointer.

## Risks / Trade-offs
- A stale or superseded plan could submit the wrong action. Reuse all existing generation and action checks before automatic confirmation.
- Events may arrive before the action response. Preserve the existing settlement ordering and resource subject across both arrival orders.
- Existing tests assert superseded text and explicit plain-install dialogs. Update only those expectations required by the approved behavior and retain their write and safety assertions.
