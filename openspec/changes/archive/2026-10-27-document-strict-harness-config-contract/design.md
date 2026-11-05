## Context

The strict harness policy applies to consumer configuration, not only the TUI.
The terminal journey already proves CLI and TUI synchronization refusal for
mixed supported/unknown harness selections and materialization targets.

## Decisions

Add the shared contract to consumer-config-format and bind the existing journey
to it. Retain the TUI interaction contract. Check harness editing before sync
so a previous invalid-configuration toast cannot satisfy the new assertion;
wait for that toast to disappear before checking the independent sync guard.
After explicit repair, open and cancel the same dialog as a positive control
and verify unchanged files before synchronization installs the pending skill.

Use Factory and Claude in both resize benchmark cases. Do not remove defensive
unknown-ID handling or change logging and pre-existing transaction recovery.
