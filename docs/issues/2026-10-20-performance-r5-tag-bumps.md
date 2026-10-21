# R5 Performance: Streaming Git Tag Bumps (R10)

## Scope And Evidence

Baseline: local `origin/main`, `99c26d07`. Implementation production blob:
`c2ff9b30451de10ce1f1f3e8cc71813599cc6819` (`internal/pipeline/update.go`).
The frozen ledger and source index remain unchanged.

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

This is performance-track work under #765, not security closure for #434.
No tag count/byte cap, timeout, cancellation contract, truncation, source
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
  A final 10-second, two-worker fuzz run passes 85,378 executions, comparing
  selected refs and the first overflowing suffix's typed error context.
- A bounded child process supplies a parse overflow followed by more than
  a pipe buffer of output. Both successful and failing Git exits complete
  with the expected error identity and no partial result. Removing the
  drain through a Go overlay makes the success-exit witness fail at its
  10-second deadline; the normal implementation passes.
- The first streaming candidate changed final-tag Unicode trimming; the
  independently authored Git test failed before the pending-line fix.
- A sandboxed CLI test starts with an actual v4 selection, generated lock,
  and materialized Claude skill, then advertises both new content at `v2`
  and an overflowing tag. Refusal retains repository membership, config
  and lock bytes/modes/mtimes, native link target, and native payload
  bytes/mode/mtime/inode, with no config-staging residue. Git cache writes
  and ordinary logging are explicitly outside that no-publication claim.

## Measurements

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

The performance and combined E2E evidence includes main at `873d0b22`.
The security integration includes main at `2d6310d4`; the measured production
blob is unchanged by those main merges. The recorded performance baseline
remains `99c26d07`.

- `make validate TEST_PACKAGES=./internal/pipeline` passes all selected
  short-mode pipeline tests, whole-root vet, CI lint (zero issues), and build.
- CLI `TestUpdate*` and `TestCommandIsolationGuard` pass freshly, including
  successful bump/scope/target/exact-pin controls and the new refusal test.
- Final focused pipeline tests, differential fuzzing, and focused race
  tests pass. Race tests use `CGO_ENABLED=1`.
- `make spec-check` passes: 64 items, 1,105 scenarios, 1,766 annotations,
  and the two pre-existing active changes. Pinned npm tooling runs offline.
- `make validate-e2e` passes: 3,389 invocations, all 113 command requirements
  covered, zero listed gaps, and all three required refusal witnesses ran
  and passed. Native harness probes are explicitly unavailable in this
  configuration-only gate.
- `make tui-qa` passes all 33 current real-terminal journeys and four QA
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

Container static checks, the documentation build, and local compatibility
validation pass before the toolchain integration. The local Go 1.27.1
compiler is also affected and below the new minimum; those checks are
not patched-compiler security evidence. CI verifies the Go 1.27.3 integration.
The unchanged audit tool separately passes trace, archive, and committed
gate checks after integration.
The subsequent main drift-fix integration at `93f7fe53` leaves that blob
unchanged; its audit checkpoint passes 1,111 scenarios, 1,772 annotations,
two active changes, and the committed gate.
The benchmark measurements remain historical Go 1.27.1 comparisons of the
unchanged production blob, not a performance claim about the patched compiler.

## Adversarial Review

Host-driven `open-code-review-delegate` supplies deterministic file
selection and rule resolution; the host reviews the implementation,
consumer paths, tests, design, this report, and raw measurements, including
OCR's default exclusions. Coverage: 13 total files, 13 reviewed, 0 skipped,
100%. No unresolved actionable critical/high/medium finding remains.

Review checks include overflow/error precedence, pipe lifecycle, borrowed
scanner-buffer lifetime, Unicode/CR trimming, accepted long tags,
publication refusal, command-test isolation, and benchmark overclaims.
The TUI assertion review checks the cold filesystem baseline, settled
input, actual post-toggle frame, and unchanged timing and acquisition controls.
The security integration review checks the unchanged production blob and
audit tool, passing container static guards, and compiler-specific claims.
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
The requested prefix-byte hoist is not adopted either: the pre-security-update
CI Go 1.26.9 compiler reports a non-escaping, zero-copy conversion, and recorded
allocations stay at 55 across 32, 2,048, and 16,384 ignored tags.
CodeRabbit's supported-Linux portability finding is fixed by resolving
`cat` through `PATH` in the drain fixture instead of requiring `/bin/cat`.
A failed tail read exits unsuccessfully so the fixture cannot pass without
the payload that makes draining necessary. Fresh focused race checks and
both negative-overlay controls pass after that fixture change.

This is local slice evidence, not an integrated-main disposition or an
external issue-status update. The remaining R5 decision gates stay open.
