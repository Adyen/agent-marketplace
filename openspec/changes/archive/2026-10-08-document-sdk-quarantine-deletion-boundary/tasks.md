## Implementation

- [x] Record the approved narrow exclusion, residual risk, alternatives, and reopening conditions in an accepted ADR.
- [x] Clarify the materialization requirement and existing scenario without changing runtime behavior or its test annotation.
- [x] Existing bound regression passed with `CGO_ENABLED=0 go test ./internal/materialize -run '^TestSDKQuarantineRemovalPreservesReplacementGeneration$' -count=1`. `make validate` passed (documentation-only automatic test selection); lint source-mapping warnings were resolved by a fresh-cache `make ci-lint` run with zero issues. `make spec-check` passed with 455 scenarios and 654 annotations.
- [x] Pre-push adversarial review of the staged seven-file documentation diff against `79aaf0f9`: all seven files reviewed, zero skipped, implementation and bound regression checked, no actionable contract/security findings. Corrected the noted archive-checklist bookkeeping; no atomicity or broader exclusion claim accepted.
- [x] Archived with pinned OpenSpec 1.12.0 using `openspec archive document-sdk-quarantine-deletion-boundary --yes`; post-archive `make spec-check` passed (455 scenarios, 654 annotations). The documentation and archive are included together in the final commit.
