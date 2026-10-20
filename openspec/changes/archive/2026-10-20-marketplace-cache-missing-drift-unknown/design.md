## Context

`list`, `status`, and `doctor` resolve Marketplace selections with cache-only artifact resolution. A pin whose revision metadata or collection manifest is absent from the cache yields a soft never-acquired warning; a pin whose cached metadata lacks a verified artifact yields a hard cache-evidence error. Neither contributes desired artifacts. `status` already skips its dry run on hard resolve errors and prints `Unknown (exact registry view incomplete)`, but runs it on the soft warning. `list` always runs the dry run.

## Goals / Non-Goals

**Goals:** report unknown sync state with the `agent-marketplace sync` remedy from `list` and `status` whenever Marketplace cache evidence for a selected pin is missing.

**Non-Goals:** `sync --check`, `doctor` (its cache resolution feeds only prerequisite warnings, which already suppress pending pins), the TUI, enumerating collection members without a cached manifest, per-pin partial drift, ownership state and lock formats.

## Decisions

- **Whole-verdict unknown.** Ownership state records resource names, not the pin that produced them, and a collection's members are unknown without its manifest. A pin's outputs cannot be singled out, so any unresolved pin makes the whole drift verdict unknown rather than reporting a partial count that may be wrong.
- **One shared predicate.** The pipeline exposes one check over resolve errors that is true for the never-acquired warning and the cache-evidence error. Inspection skips the dry run when it is true, and `list` uses the same check instead of running its dry run. `status` chooses the unknown DRIFT wording from it.
- **Exit codes unchanged.** The never-acquired warning stays soft; the missing-artifact error stays hard.

## Risks / Trade-offs

- Real drift unrelated to Marketplace pins is hidden while the cache is incomplete. The remedy, `agent-marketplace sync`, restores evidence and a precise verdict.
