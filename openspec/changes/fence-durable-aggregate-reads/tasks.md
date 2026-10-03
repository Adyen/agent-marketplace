## Implementation

- [x] Prove store-relative alias and actual Gemini root-retarget reader regressions red/green.
- [x] Route every durable reconciliation/repair/CAS/backup/recovery aggregate read through recorded-root captures.
- [x] Verify aliases/absence/traversal/FIFO and native controls on Linux/macOS with races.
- [x] Measure scoped reader disk cost; run validate/spec and all-entry adversarial review.
- [ ] `openspec archive fence-durable-aggregate-reads --yes`.
