# Run isolated analyzer Jobs for indexed skills

## Why

Marketplace should scan every indexed skill, including skills from third-party registries that provide no scan reports. Today the supplied Kubernetes deployment expects a Marketplace-specific HTTP runner that neither the Marketplace release nor NVIDIA publishes, and skills near the end of a refresh can miss their scan when the registry time budget expires.

## What Changes

- Add an opt-in Kubernetes Job execution mode. Marketplace schedules each configured analyzer for each indexed skill revision, in both index-only and mirror acquisition. The existing built-in decision continues to govern publication and installation.
- Start with the unmodified SkillSpector CLI: give it isolated files and read its native JSON. Neither the worker image nor the registry repository supplies scanner code, a custom server, or credentials to the analyzer.
- Record work independently of the indexing deadline. Repeated refreshes reuse completed work for the same revision and analyzer configuration; late results cannot become current for a different revision.
- **First milestone:** prove the input and output handoff for one index-only skill in a disposable cluster before implementing the general scheduling path. Keep the current runner, Compose, and Helm defaults operational during evaluation.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-analysis`: account for each configured analyzer on each indexed skill and bind native results to the revision that was scanned.
- `marketplace-delivery-security`: run an operator-selected CLI image as a separate, offline Job without making an index-only artifact downloadable.

## Impact

Touches Marketplace ingestion, advisory report parsing, assessment publication and the Kubernetes deployment. The proposed mode requires an enforcing NetworkPolicy implementation and temporary isolated storage. It does not introduce a public report submission API, change consumer configuration, or depend on a registry's CI. No default cutover or removal of the existing runner is proposed here.

## Release note

Kubernetes operators will be able to scan indexed skills with an isolated SkillSpector CLI image, without installing scanner dependencies in the Marketplace worker. Supplementary scan results will remain advisory.
