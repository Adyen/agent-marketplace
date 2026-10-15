## 1. Tests first

- [x] 1.1 Add command tests in `cmd/agent-marketplace` covering every scenario of the `resource-selection` delta, each bound with a `// spec: resource-selection/<scenario-slug>` line, and observe them fail on the base

## 2. Pre-publication selection guard

- [x] 2.1 Add an optional selection guard to the lock lifecycle that runs after new snapshots are verified and before the dependency transaction commits, only in upgrade mode when a lock would change
- [x] 2.2 Resolve prior and desired lock states without recording inventory, and diff not-found diagnostics by kind, name, canonical URL, path, and collection name
- [x] 2.3 Return a typed refusal naming each newly missing selection once with the fixed hint, and surface it unwrapped from `update`

## 3. Verification

- [x] 3.1 Focused update tests, `make validate`, `make spec-check`, and `openspec validate --all --strict` pass
