## Context

Ordinary synchronization refuses hard pre-materialization errors. Inspection also
supports partial inventory for interactive browsing, and its default dry run can
therefore interpret an unloadable registry as an empty selection.

## Decisions

Enable the existing strict inspection option at the synchronization-check
consumer, matching status. Keep partial inspection available to the TUI, whose
inventory/recovery flow has a different purpose. Do not suppress the resulting
hook review after an unsafe dry run; prevent that dry run instead.

Continue reporting original hard errors. Soft catalog/resolution warnings must
not disable legitimate drift checks, and materialization-time errors must still
be discovered by checks that passed pre-materialization validation.

## Validation

Enable the existing real-binary unloadable-registry witness. Exercise Factory's
native hook entries and Codex's durable ownership state, in repository and
machine scopes. Fingerprint installed files, configuration roots, and existing
ownership state across both refused commands, including bytes, mode, and mtime.
After repairing the source, require replacement and deselection reviews, prove
checks are read-only, and verify that ordinary sync applies those operations.

Repair the existing local-registry synchronization-check benchmark to use valid
v4 fixtures, retain isolated paths, and compare before/after measurements with
disk I/O included and no network requests.

## Implementation Evidence

All four Factory/Codex and repository/machine regression cases failed on the
original check path and passed with strict refusal enabled, including genuine
replacement/removal controls. Nearby ambiguous-source, malformed-envelope, and
timeout E2E witnesses and the command synchronization-check regressions passed.

On darwin/arm64 (Apple M4 Max), five two-second benchmark samples per case
measured median latency of 57.52 to 62.55 ms for 10 resources and 218.2 to
237.1 ms for 50 resources. Both timing comparisons have p=0.151; five samples
do not establish a 95% confidence interval. Bytes and allocations per operation
are statistically unchanged. This is not a speedup or a demonstrated latency
regression. The timed path includes local disk I/O but no network requests;
fixtures and logging behavior are identical within the comparison.

Read-only adversarial and OCR delegate review covered all nine intended files
against main `6e7f0134`, including default-excluded tests, benchmarks, and
specification/documentation files. No critical, high, or medium findings
remained. The checklist is updated before archival; existing review-output
assertions intentionally retain the current user-visible contract.
