## 1. Tests first

- [ ] 1.1 Add failing command tests with `// spec: deployed-acquisition-locks/<scenario-slug>` annotations for every scenario of the explicit organization relock requirement
- [ ] 1.2 Confirm they fail on the base for the intended reason (`lock.stale` from the explicit organization write)

## 2. Implementation

- [ ] 2.1 Let the explicit organization lock write load configuration without the organization lock's configuration-match check, keeping decode, scope, and trust validation
- [ ] 2.2 Confirm plain and `--upgrade` reconciliation reuse `reconcileLayerDocument` and that consumer and read-only paths still refuse `lock.stale`

## 3. Verification

- [ ] 3.1 Focused command and pipeline tests pass; `make validate` and `make spec-check` pass
