## Context

Inspect-mode resolution returned metadata-only Marketplace artifacts, which selection filters out, and it did not install the transient-organization Marketplace resolver, so the desired resource set never contained Marketplace selections. The `status` dry run therefore treated their materialized outputs as orphans, and `doctor` never saw the kinds and names a Marketplace selection provides. `sync --check` resolves the same configuration with full artifact selection, which is why the two commands disagreed.

## Decision

`status` and `doctor` opt into Marketplace artifact selection from the verified local cache: the pins table keeps its existing inspection source, while artifact selection delegates to the Marketplace resolver in cached, no-fetch mode — the same evidence offline sync uses — and is wrapped in the transient-organization resolver when the organization lock is intentionally absent, so organization selections expand exactly as in sync. Required-pin health errors from the inspection merge into the selection's errors, deduplicated per resource coordinate.

Evidence tiers stay distinct. A pin that was never acquired keeps its inspection-only outcome: a visible remediable warning, the pins table, and the existing exit status. A pin with cached revision metadata but no verified artifact is a hard error naming the resource coordinate and `agent-marketplace sync`. Status skips the materialization dry run on resolve errors, so an unresolvable pin's outputs are never reported as orphans. Only `status` and `doctor` set the option; `list`, `deinit`, `remove` cleanup, `sync`, and the TUI keep inspect-mode defaults.

## Verification

Command tests cover the synced pin (SYNCED RESOURCES, no orphans, no drift, no artifact downloads, unchanged configuration and lock bytes, agreement with `sync --check`), the organization collection without an organization lock, the unverified-artifact hard error, and `doctor`'s Marketplace-satisfied prerequisite. Adapter unit tests cover required-pin health-error merging, cached-no-fetch enforcement, the missing-evidence tier split, and the transient-organization wrapper. The existing never-synced pins regression and the full status and doctor suites keep passing.
