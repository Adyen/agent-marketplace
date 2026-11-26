## Context

The current text path prints the normal doctor inventory before repair. Each diagnosis prints the plan; the aggregate preview decides independently whether to print `nothing to repair`. Nested sync reports native link installations, which is not the result of repairing cache content behind an unchanged link.

## Goals / Non-Goals

Make the confirmation legible and the final verdict truthful. This is a presentation change, not a fix for Python regenerating bytecode, a relaxed ownership check or a new repair step.

## Decisions

Both views use the same planner and the same re-diagnosis after each step. Render a compact text view by default, an opt-in `--verbose` view for troubleshooting, and the existing JSON view for automation. Track presentation separately from step identity and consent. Use the re-diagnosed plan to distinguish cleared drift from an attempted sync; do not infer cache writes, causes or success by parsing `Installed` counts. Keep errors, safety guidance, per-entry destructive prompts and fresh backup requirements at the point of action. Preserve journal outcome reporting. The detail flag must not add prose to JSON.

### Review walkthrough (mockup; wording illustrative)

Before: inventory for eight harness targets, two planned actions, repeated warnings and empty file inspections, then a prompt. With only drift left, the aggregate scan says `nothing to repair` before the question.

```text
2 safe repairs
1. [safe] Refresh Devin user skill caches: enqueue, ship-it.
2. [safe] Record ownership in ~/.factory/mcp.json: issues, metrics, inventory.
   These entries match their selected resources; the file contents will not change.
Advisory warnings: use doctor --repair --verbose for details.
Apply both safe repairs? [y/N]
```

After consent and a clean recheck: name the cleared skills, report the adopted entry count and file, then say no repairs remain once. If `enqueue` remains, name it as remaining rather than installed or fixed. Declining applies nothing; a destructive replacement still requires its own per-entry confirmation. In this example the entry names equal their resource names; identify both when they differ.

## Risks / Trade-offs

Collapsing diagnostics can obscure a refused write or leak values through an alternate rendering path. Preserve error codes and remedies for actionable failures, and keep conflicts digest-only in both views. Do not hide a new consent decision merely because another step looks similar. Keep the full scan available to distinguish a clean file from one never inspected.
