## Context

The report parses Go samples and independently tests time, bytes, and allocation medians. With many comparisons, unadjusted p-values lead to false highlights; differences do not establish causation.

## Goals / Non-Goals

Goals: one family of all comparable metric tests, concise non-causal output, inspectable collapsed evidence. Non-goals: CI planning, sampling order/count/duration, raw Mann-Whitney implementation, and benchmark workloads.

## Decisions

Use Holm adjusted p-values: sort all comparable raw p-values, multiply each by the remaining family size, take cumulative maxima and cap at one. Include unchanged metrics and effects below the notable threshold; exclude added/removed benchmarks. Preserve raw p-values separately. Use adjusted p<0.05 throughout percentage rendering and retain >=10% for highlights. Count highlighted metric increases and decreases independently, including mixed directions in one benchmark. Keep the notable table compact; append base/head sample counts and per-metric adjusted p-values to the collapsed full table. Explain insufficient evidence rather than equivalence and retain a brief runner caveat outside details.

Unadjusted testing was rejected because it ignores multiplicity. Changing sampling was excluded by user scope.

## Risks / Trade-offs

Six samples may provide insufficient power after correction; this is not proof of unchanged performance. Holm does not remove temporal runner bias. The existing normal-approximation Mann-Whitney raw test remains unchanged, so significance is evidence under its assumptions, not proof of PR causation.
