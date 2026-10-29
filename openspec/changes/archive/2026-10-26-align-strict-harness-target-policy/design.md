## Context

Configuration rejects unknown harness selections and per-target
materialization settings. The TUI spec instead promised preserving unknown
selected IDs; model tests bypassed loading to manufacture that state.

## Goals / Non-Goals

Keep strict runtime behavior, make the specification truthful, and prove the
user-visible refusal. Do not add warning-only forward compatibility, broaden
accepted configuration, or remove defensive form code merely because model
injection can reach it.

## Decisions

ADR 0010 selects option 1: **Your CLI must understand every tool named in the
configuration.** A supported/unknown mixture is invalid, not permission to
operate on a supported subset. Refusal does not edit away the unknown entry.

Keep save/resize scenarios with supported IDs and test the actual input
boundary separately. Real-terminal cases use otherwise valid v4 configuration,
an existing installed resource, and an additional pending resource. They check
the visible error, blocked sync, preserved files and successful supported-input
recovery. Reuse the existing fixture and read helpers.

## Risks / Trade-offs

Mixed CLI versions cannot share target selections the older CLI does not
understand. Users must upgrade or explicitly correct/remove an entry. This is
existing behavior, not a newly introduced refusal.

No blanket no-write promise is added to logs or recovery of a pre-existing
dependency transaction. Required format and recovery contracts remain intact.
