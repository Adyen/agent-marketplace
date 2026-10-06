## Context

fileMatchesBytes is shared SDK Materialize artifact comparison. os.ReadFile can accept hardlinks or block on FIFO even though wrappers have their own boundary checks. A native sync test initially passed on the parent because an earlier ownership guard refused, so public SDK Materialize is the actual reproducing consumer.

## Decision

Capture the comparison parent, classify the leaf through its NativeRoot, read through existing Dir.ReadFile with captured-size limit, and require returned opened identity to match original full state. The reader admits only singly linked regular files and uses nonblocking admission for special files. One payload descriptor supplies bytes; no new limit. Missing safe parents/leaves remain ordinary absence. Named result joins close failures without hiding primary refusals. Embedded SDK reuses internal/fsboundary rather than a second filesystem implementation. Publication/discovery authority remains a separate audit.

## Verification

Public SDK Materialize first creates an exact inert 0640 artifact; a second invocation with an extra hardlink formerly reported skipped/success (red). Refusal is ErrUnsafeFile, preserves foreign bytes, inode, mode, mtime and output membership, and creates no staging. Removing the extra link permits an unchanged no-op (identity, mode and mtime preserved) and an actual independently authored payload update with 0640 mode. FIFO public SDK refusal is bounded in a child process and preserves its generation and membership. Required: Linux/macOS full materialize/SDK translation suites, repeated races, validate/spec/shellcheck, full RunSupportFiles disk lifecycle costs, and a complete all-file review. No new public schema, dependencies, limits, source comments or whole-tree transaction. Passed: Linux/macOS full materialization/SDK translation; count-20 race, API, FIFO, source and cache controls; validate, lint, build, spec and shellcheck.

Benchmark (full disk RunSupportFiles, serial; 2s × 5 runs; no 95% CI):

- Time: 11.427634 ms → 22.161766 ms (+93.93%, p = 0.008)
- Memory: 472842 → 486238 B/op (+2.83%)
- Allocations: 5328 → 5464 allocs/op (+2.55%)

An earlier baseline of 8.991031 ms and the 8–32 ms sample spread are retained and disclosed; this lifecycle cost is not an isolated comparator microbenchmark. Timed: source, staging, cache, prune, copy and native I/O. Excluded: network (none), logging (discarded) and setup.

The frozen #540 combined E2E/configuration gate (471.045 s) plus shellcheck passed; the SDK writer/discovery audit remains separate.
