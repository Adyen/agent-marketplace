## Why

PR #622 changed only release automation but its benchmark comment labeled unchanged benchmark source as regressions and improvements. Independent significance tests across many metrics and causal wording overstate the evidence.

## What Changes

- Apply Holm correction across all comparable benchmark/metric tests before displaying significance.
- Retain the 10% notable-effect threshold and use observed increase/decrease wording.
- Keep sample counts and adjusted p-values inside collapsed details, with a short runner caveat outside.
- Leave CI selection, sampling, workloads, and raw Mann-Whitney computation unchanged.

## Capabilities

### New Capabilities
- `benchmark-reporting`: Concise evidence-qualified benchmark comparison comments.

### Modified Capabilities
None.

## Impact

Only the benchmark-report tool, its tests, and OpenSpec reporting contract change. No runtime CLI behavior or dependencies change.
