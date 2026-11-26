# Design

## Context

`runSyncCheck` permits transient organization evidence, but the repair closure calls strict locked sync. The repair path must preserve the acquisition locks and the existing ordering between drift reconciliation and shared-aggregate ownership repair.

## Goals / Non-Goals

**Goals:**

- Reconcile harness drift using the same verified transient organization Marketplace evidence used for diagnosis when no organization acquisition lock is deployed.
- Preserve existing acquisition locks and continue to later eligible ownership repair.

**Non-Goals:**

- Change standalone sync modes.
- Change organization policy.
- Change unrelated output.

## Decisions

- Add a doctor-only internal locked sync mode that admits the already-verified transient organization acquisition evidence from its command context, and only when deployed organization acquisition evidence is absent.
- Keep existing lock leases, policy generation assertions, verification, and collision handling.
- Do not call unlocked sync, which can write writable acquisition locks.
- Describe this as a doctor repair step, not a literal standalone command, when transient evidence is needed.

Proposed plan example: the plan lists safe drift reconciliation followed by safe adoption; after confirmation, both complete. If verification is unavailable, selection-dependent steps do not run.

## Risks / Trade-offs

- [Authority changes after confirmation or verification fails] → Refuse the affected selection-dependent steps and preserve the staged repair and refusal semantics.
