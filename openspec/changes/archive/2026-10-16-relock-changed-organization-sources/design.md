## Context

`lock --scope organization` calls `prepareExactLocks` with `explicitOrganization: true`. Before it reconciles any layer, it loads the effective configuration through `pipeline.LoadEffective`, whose `readDeployedLocks` returns `lock.stale` for a present organization lock that does not match the organization configuration. The load error is returned, so the reconciliation step never runs. That step, `reconcileLayerDocument`, already keeps recorded `(url, ref)` entries that are still configured, resolves the missing ones, and drops the rest; `--upgrade` resolves all of them.

## Goals / Non-Goals

**Goals:** let the explicit organization lock write reconcile a stale but valid organization lock; keep every other path refusing it.

**Non-Goals:** implicit organization lock handling, `lock --check --scope organization` reporting, `update`, other layers, lock format, and the day-2 operations docs.

## Decisions

- Add an option to configuration loading that skips only the organization lock's configuration-match check. Decode, scope, and organization trust validation stay in force. Only `prepareExactLocks` sets the option, and only for an explicit organization write (`mode.explicitOrganization` and not `mode.check`). With the check skipped, the organization layer binds without deployed lock evidence for that load, and `prepareExactLocks` reads and reconciles the lock itself, as it already does for writable layers.
- Reuse `reconcileLayerDocument` unchanged. It already gives the required plain and upgrade semantics, so no new reconciliation code is needed.
- Publication keeps going through the existing dependency transaction, so a failed resolution leaves the lock untouched.

## Risks / Trade-offs

- Skipping the match check removes the guard against a load that binds stale organization evidence. The option is confined to the explicit write, which never binds the deployed organization lock as resolution authority before reconciling it.
