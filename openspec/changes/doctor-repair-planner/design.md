## Context

See proposal.md for motivation. The approved design and work breakdown live in `docs/plans/2026-11-11-doctor-repair-planner-design.md`; this file records only the decisions the specs depend on.

## Goals / Non-Goals

**Goals:** one repair loop that every later fixer plugs into without changing how plans are shown, confirmed, or exited.

**Non-Goals:** setting aside a broken personal configuration layer, TUI integration, automatic v4 conversion, and any change to organization files or harness lists.

## Decisions

- **Loop, not one-shot plan.** Repair runs one step, diagnoses again, and recomputes the plan, because fixes cascade (a journal replay can clear a stale lock). A one-shot plan from one diagnosis was rejected.
- **One diagnosis source.** The plan is built from the same configuration, resolution, journal, and ownership data plain `doctor` loads, so `doctor` and `doctor --repair` cannot disagree.
- **Stage order.** Journal recovery, configuration, acquisition locks, cache and snapshots, harness drift, shared-aggregate ownership. The drift stage comes before ownership repair so sync output exists before ownership candidates are computed.
- **Confirmation.** One plan-level confirmation authorizes the safe steps; destructive entries keep per-entry prompts; new steps after re-diagnosis are confirmed separately. A plan with only destructive entries asks only the per-entry prompts, which keeps the existing one-answer-per-entry contract. Per-step confirmation for every step was rejected as tedious.
- **Degrade per step.** Each step declares its inputs. A step with invalid inputs is listed as blocked by the item that reports the cause; causes without a fixer appear as `human-only` items with the existing remedy text.
- **Recurrence cap.** A step identified by stage, action, code, and files that is still planned after five runs stops the repair, so a non-converging fix cannot loop forever.
- **Policy authority.** The configuration and acquisition-lock read assertions captured at diagnosis are revalidated before every confirmed step and at every publication boundary. No ownership lock is held while waiting for confirmation.

## Risks / Trade-offs

- [The drift step could fight ownership repair over an unmanaged collision] → unmanaged shared-aggregate collisions are excluded from drift and left to the ownership stage.
- [An extra plan-level prompt could shift answers for existing per-entry prompts] → no plan-level prompt is asked when no safe step is runnable, and existing repair tests run unchanged.
