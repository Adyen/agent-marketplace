# R5 Performance: Streaming Git Tag Bumps (R10)

## Scope And Evidence

Original baseline: local `origin/main`, `99c26d07`. Initial streaming
measurement blob: `c2ff9b30451de10ce1f1f3e8cc71813599cc6819`.
The compatibility-restored production blob is
`9d3f695ed5a6727f7a4b70d3059ecb7830069b00` (`internal/pipeline/update.go`).
Its current Go 1.27.3 comparison is recorded separately below; the original
Go 1.27.1 measurements, frozen ledger, and source index remain unchanged.

R10/A47/F31 has instance ID
`01a0a06b-ae64-74f1-b1c4-036b618c1260`. Both original exports were checked
against the frozen hashes and both versions of this finding were read.
They make the same full-tag-output buffering complaint.

The supported path is CLI `update --bump`, `pipeline.Update`,
`bumpRepoRegistryRefs`, then `discoverTagBump`. A configured registry supplies
the tag names. Previously `git tag -l` stdout was accumulated, converted to
a string, and split into a complete tag slice before selecting one ref.
Bounded local fixtures reproduce the allocation growth without exhausting
memory, executing resources, or contacting external services.

The implementation scans the unchanged Git command's output, retaining the
scanner buffer, one pending line, and the best candidate instead of the
full tag set. The pending line preserves the old whole-output whitespace
trimming, including Unicode whitespace in otherwise accepted Git refs.
Scanner growth is not restricted to its default 64 KiB token ceiling.
On a parse refusal, remaining output is drained before waiting for Git;
a failing Git exit still takes precedence and no partial bump escapes.
Git exit failures retain `Output`'s typed `ExitError.Stderr` payload:
its existing 32 KiB prefix/tail capture and omission marker. This is not
unbounded stderr buffering or a new public diagnostic format.

This is performance-track work under #765, not security closure for #434.
No new tag count/byte cap, timeout, cancellation contract, stdout truncation, source
ownership rule, config/lock format, or publication policy changes.
Git acquisition, Git's own sorting/storage costs, and D5 remain separate.
The implementation does not establish a hard memory bound: a single long
tag can still require a large buffer, and successively better numeric tags
can allocate replacement candidate strings.

`Spec-Impact: none` applies because observable selection, error identities,
and publication behavior are preserved. No new normative scenario or
OpenSpec delta is claimed.

## Preservation And Adversarial Controls

- Real local Git tests retain trailing-integer families, numeric rather
  than lexical ordering, empty/non-version inputs, equal-current results,
  lexical ties, authored whitespace, numeric overflow, and Git exit errors.
  Three configured Git sort orders and a tag longer than 70,000 bytes run.
  A Go overlay restoring the default 64 KiB token limit makes that long-tag
  witness fail with the scanner's token-too-long refusal.
- Stream controls cover one-byte reads, absent final newlines, invalid
  UTF-8, carriage returns, and whole-output versus interior Unicode
  whitespace. Read failure discards an otherwise valid candidate.
- The frozen buffered-selection oracle is independent of the new scanner.
  The original Go 1.27.1 ten-second, two-worker fuzz run passes 85,378
  executions; a fresh Go 1.27.3 run passes 81,139 after the stderr repair.
  Both compare selected refs and the first overflowing suffix's typed error context.
- A bounded child process supplies a parse overflow followed by more than
  a pipe buffer of output. Both successful and failing Git exits complete
  with the expected error identity and no partial result. Removing the
  drain through a Go overlay makes the success-exit witness fail at its
  10-second deadline; the normal implementation passes.
- Gemini identified a lost typed `ExitError.Stderr` payload when replacing
  `Output`. An independent real-Git oracle fails before its restoration:
  the streamed error has empty stderr instead of Git's original fatal text.
  Differential process tests now retain nil/binary stderr, exact 32/64 KiB
  boundaries, a 1 MiB payload, the omission marker, Git-error precedence
  over a parse refusal, and the unchanged public error message. Writer
  fragmentation down to one byte is compared with `exec.Cmd.Output`;
  focused normal and race checks pass with Go 1.27.3.
- Real-Git fixtures isolate HOME, global/system configuration, explicit
  repository/object/configuration environment, and ancestor discovery.
  Controlled regressions poison `GIT_DIR` plus injected config, and put
  `TMPDIR` inside an enclosing repository. Disabling the isolator makes
  both select the ambient `v9` instead of the fixture's `v2` or exit 128.
  Normal and race checks pass with isolation; no caller repository is used.
- The first streaming candidate changed final-tag Unicode trimming; the
  independently authored Git test failed before the pending-line fix.
- A sandboxed CLI test starts with an actual v4 selection, generated lock,
  and materialized Claude skill, then advertises both new content at `v2`
  and an overflowing tag. Refusal retains repository membership, config
  and lock bytes/modes/mtimes, native link target, and native payload
  bytes/mode/mtime/inode, with no config-staging residue. Git cache writes
  and ordinary logging are explicitly outside that no-publication claim.

## Measurements

### Historical Go 1.27.1 Snapshot

These original samples measure the initial streaming snapshot before the
typed-stderr repair, not the final compatibility-restored implementation.
Use the Go 1.27.3 comparison below for current headline allocation claims.

Five two-second samples per size, macOS arm64, Apple M4 Max, Go 1.27.1,
`CGO_ENABLED=0`. Each size is the number of matching but nonnumeric tags
with 96-byte name padding, plus one valid `v1.2` control. Thus size zero
is a one-tag control, not an empty repository.

The timed path includes Git process startup, local packed-ref reads,
stdout transport, and selection. It excludes fixture construction,
clone/fetch, configuration edits, dependency reconciliation, and native
materialization. Allocation metrics describe the Go parent process, not
the Git child's RSS or total peak memory. These measurements do not claim
an end-to-end update latency improvement or flat allocations for every
possible numeric-tag ordering.

| Nonnumeric tags | Before ns/op | After ns/op | Before B/op | After B/op | Before allocs/op | After allocs/op |
|---:|---:|---:|---:|---:|---:|---:|
| 0 | 11,315,133 | 11,496,534 | 64,125 | 57,964 | 75 | 54 |
| 32 | 10,796,736 | 11,660,817 | 82,199 | 57,973 | 78 | 55 |
| 2,048 | 12,446,951 | 13,582,375 | 875,114 | 57,893 | 85 | 55 |
| 16,384 | 21,439,716 | 23,549,924 | 6,445,220 | 58,367 | 91 | 55 |

The largest fixture reduces Go allocation bytes by 99.09% and allocation
count by 39.56%. Its median time increases by 9.84% (`p=0.548`, `n=5`);
the 32-tag case increases by 8.00% (`p=0.032`). This is an allocation
improvement with a measured latency tradeoff, not a speed-up claim.
Benchstat warns that five samples cannot supply its 95% confidence interval.
No timing threshold enters the tests.

Raw before/after samples and benchstat output:
`docs/issues/performance-r5-tag-bumps-2026-10-20/`.

```sh
CGO_ENABLED=0 go test ./internal/pipeline -run '^$' \
  -bench '^BenchmarkDiscoverTagBump$' -benchtime 2s -count 5 -benchmem
benchstat docs/issues/performance-r5-tag-bumps-2026-10-20/before.txt \
  docs/issues/performance-r5-tag-bumps-2026-10-20/after.txt
```

### Compatibility-Restored Go 1.27.3

Five fresh two-second samples use the same fixtures, machine, flags, and
timed boundary with Go 1.27.3. The buffered baseline restores the verbatim
`discoverTagBump` and `listGitTags` functions from main `2d94c12d` through
a Go overlay. The existing streaming helper and pre-stderr test harness
from `2a72a7f5` remain only to compile the permanent benchmark; they are
not called on the buffered timed path. The comparison therefore includes
the original stderr capture on both sides and changes no fixture or ref.
The current production source is the compatibility-restored blob above.
These samples use the fixture harness at `485af695`, before the later
test-only ambient-Git isolation. That hardening does not change production
source; the recorded samples are retained, not rerun or relabeled.

| Nonnumeric tags | Buffered ns/op | Restored stream ns/op | Buffered B/op | Restored stream B/op | Buffered allocs/op | Restored stream allocs/op |
|---:|---:|---:|---:|---:|---:|---:|
| 0 | 12,434,177 | 12,596,004 | 62,561 | 64,345 | 79 | 73 |
| 32 | 12,068,588 | 12,450,512 | 81,898 | 64,378 | 82 | 74 |
| 2,048 | 14,717,098 | 14,283,528 | 875,827 | 65,229 | 89 | 74 |
| 16,384 | 24,402,217 | 25,009,065 | 6,446,199 | 65,360 | 95 | 74 |

The largest fixture reduces allocation bytes by 98.99% and allocation
count by 22.11%. Its observed median time rises 2.49%, with no significant
difference (`p=0.690`, five samples). The 32-tag case rises 3.16%
(`p=0.008`); the one-tag control allocates 2.85% more bytes. These costs
are retained in the evidence, not described as an end-to-end speed-up.
Five samples still cannot supply benchstat's 95% confidence interval.

The first bounded-stderr repair used `io.Copy`'s default read buffer,
raising the one-tag fixture to 94,391 B/op and the largest to 96,115 B/op.
Those samples and their comparison are retained as
`streaming-stderr-default-buffer-go1.27.3.txt` and
`stderr-default-buffer-benchstat-go1.27.3.txt`, not passed off as final code.
The final `ReaderFrom` uses a 4 KiB copy buffer instead of duplicating the
scanner's 32 KiB allocation even when Git emits no stderr. This changes
transport buffering, not the existing captured-stderr bound or tag policy.

Current raw samples are `buffered-go1.27.3.txt` and
`streaming-stderr-go1.27.3.txt`; their comparison is
`stderr-benchstat-go1.27.3.txt` in the same evidence directory. The benchmark
command and sample count remain those above; only the baseline uses the
described source/test overlay.

### ASCII Validation Follow-up

Gemini suggested replacing rune decoding with a byte loop for the
ASCII-only numeric suffix. This preserves the predicate for every byte
sequence, including invalid UTF-8 and non-ASCII digits. The final
end-to-end samples above include that follow-up.

`BenchmarkSelectTagBumpNumericSuffix` separately measures in-memory
selection of one numeric tag with 8, 128, or 70,000 leading zeroes followed
by `2`. Its input, reader, and expected selection remain identical across
the rune and byte implementations; there is no Git process or filesystem
in this timed path. Five two-second samples compare the streaming rune
implementation (`89fb14ab4428ab19199a247a75717fd45427ebd5`) with the final
byte-loop implementation, not with the original buffered Git path.

| Leading zeroes | Rune median ns/op | Byte median ns/op | Change |
|---:|---:|---:|---:|
| 8 | 1,140 | 1,186 | No significant difference, p=1.000 |
| 128 | 1,485 | 1,321 | -11.04%, p=0.008 |
| 70,000 | 194,043 | 109,951 | -43.34%, p=0.008 |

Allocation bytes and counts are identical in every selector fixture.
The eight-zero case is noisy in both runs; all samples are retained.
These are numeric-validation microbenchmarks, not evidence of a faster
end-to-end update. Raw samples and comparison are the `selector-*.txt`
files in the same evidence directory.

```sh
CGO_ENABLED=0 go test ./internal/pipeline -run '^$' \
  -bench '^BenchmarkSelectTagBumpNumericSuffix$' -benchtime 2s -count 5 -benchmem
benchstat docs/issues/performance-r5-tag-bumps-2026-10-20/selector-before.txt \
  docs/issues/performance-r5-tag-bumps-2026-10-20/selector-after.txt
```

## Validation

The original evidence includes main at `873d0b22`; the historical baseline
remains `99c26d07`. Current compatibility-restored validation includes main
`2d94c12d`, the bounded-stderr repair, and the current Go 1.27.3 comparison.
The current checks use the cached patched compiler and CI-pinned linter
2.14.0 without changing global tools.

- `make validate TEST_PACKAGES=./internal/pipeline` passes all selected
  short-mode pipeline tests, whole-root vet, CI lint (zero issues), and build.
- CLI `TestUpdate*` and `TestCommandIsolationGuard` pass freshly, including
  successful bump/scope/target/exact-pin controls and the new refusal test.
- Focused stream/error tests and race checks pass with Go 1.27.3, including
  the stdout drain and bounded stderr process witnesses. Race tests use
  `CGO_ENABLED=1`. Current differential fuzzing passes 81,139 executions
  with two workers over ten seconds; historical evidence remains separate.
- `make spec-check` passes: 64 items, 1,117 scenarios, 1,779 annotations,
  and the two pre-existing active changes. Pinned npm tooling runs offline.
- `make validate-e2e` passes: 3,389 invocations, all 113 command requirements
  covered, zero listed gaps, and all three required refusal witnesses ran
  and passed. Native harness probes are explicitly unavailable in this
  configuration-only gate.
- `make tui-qa` passes all 35 current real-terminal journeys and four QA
  utility tests, with zero skips, after the cold-fixture assertion repair.
  The missing-lock fixture deliberately leaves beta unsynced. Its journey
  now proves that absent native output and waits for the initial one-item
  pending group before timing input; selecting alpha must show two pending
  items. The obsolete one-item assertion can match a pre-toggle frame; a
  control gated on the updated frame rejects it. No timing ceiling is raised.

These are scoped root checks, not `make validate-full`. No TUI behavior
change, local released-client container gate, vendor-native probe, or
PostgreSQL performance result is claimed.

### Go Security CI Repair

CI on `eed919a3` reported the newly published standard-library advisory
[GO-2026-6809](https://pkg.go.dev/vuln/GO-2026-6809): Go 1.26.9 can trust
compromised intermediate certificates. Main's PR #846 fixes this using
Go 1.27.3 across the shared toolchain and public build-image pins. This
branch integrates that upstream fix at `2d6310d4`, rather than carrying
duplicate pin changes. No custom certificate logic or scanner suppression
is introduced by this slice.

The initial container/documentation/compatibility checks used local
Go 1.27.1, which is affected and below the new minimum; those checks are
not patched-compiler security evidence. Go 1.27.3 subsequently became
available in the shared SDK cache, allowing fresh local validation without
a new SDK download. The current checks above and the new compatibility-
restored benchmark use that patched compiler. CI verifies the advisory
through `govulncheck`; no custom certificate behavior is tested or claimed.

The inherited main SDK, drift, TUI-test, and public-site integrations left
the initial streaming blob unchanged. The later typed-stderr repair is
the explicit production change between that snapshot and the final blob.
Historical Go 1.27.1 measurements are not relabeled as patched-compiler data.

## Adversarial Review

Host-driven `open-code-review-delegate` supplies deterministic file
selection and rule resolution; the host reviews the implementation,
consumer paths, tests, design, this report, and raw measurements, including
OCR's default exclusions. Coverage: 18 total files, 18 reviewed, 0 skipped,
100%. No unresolved actionable critical/high/medium finding remains.

Review checks include overflow/error precedence, pipe lifecycle, borrowed
scanner-buffer lifetime, Unicode/CR trimming, accepted long tags,
publication refusal, command-test isolation, and benchmark overclaims.
The TUI assertion review checks the cold filesystem baseline, settled
input, actual post-toggle frame, and unchanged timing and acquisition controls.
The integration review checks inherited versus own changes, passing
container static guards, and compiler-specific claims. The stderr review
checks `Wait` synchronization, ring wraparound, bounded retained bytes,
nil/binary payloads, error precedence, and the unchanged diagnostic text.
The measured time tradeoff and missing aggregate quota are not hidden.
A clean review and passing tests are not proof of defect absence.

PR #828 also receives CodeRabbit and Gemini reviews. Gemini's ASCII
validation suggestion is implemented and measured above. Its proposed
8 MiB and 10 MiB scanner ceilings are not adopted: the previous buffered
implementation had no such ceiling, and silently introducing one would
change supported inputs and require the separate D5 quota decision.
That disposition is recorded in the review threads, not presented as
security closure or denial that a single long tag can consume memory.
The further decimal-parser shortcut is not adopted: returning bare
`ErrRange` loses `Atoi`'s typed error context, and its overlay fails the
existing overflow oracle. Compiler escape analysis also confirms that
the tie comparison's temporary string does not escape.
The later manual-parser revision preserves `NumError` and is a plausible
optional follow-up, not a correctness repair. It is deferred pending its
own numeric-family measurements and int-width boundary controls rather
than replacing the standard library in this measured slice. No claim is
made that every long-numeric suffix conversion is allocation-free.
The repeated prefix-byte hoist is not adopted: current Go 1.27.3 compiler
analysis confirms the non-escaping, zero-copy conversion already reported
by Go 1.26.9 and 1.27.1. Prior ignored-tag allocations are flat at 55;
the final compatibility-restored samples are flat at 74 for nonzero sizes.
The valid typed-stderr finding is repaired as described above, without
the suggested unbounded buffer or a new diagnostic string.
CodeRabbit's supported-Linux portability finding is fixed by resolving
`cat` through `PATH` in the drain fixture instead of requiring `/bin/cat`.
A failed tail read exits unsuccessfully so the fixture cannot pass without
the payload that makes draining necessary. Fresh focused race checks and
both negative-overlay controls pass after that fixture change.

This is local slice evidence, not an integrated-main disposition or an
external issue-status update. The remaining R5 decision gates stay open.
