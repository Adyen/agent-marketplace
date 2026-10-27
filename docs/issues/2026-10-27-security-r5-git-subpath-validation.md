# R5 Allocation-Free Git Subpath Segment Iteration

## Disposition

Independent partial R53/F136 progress under #765, measured from main
`47be33f6c490e5c5c1aca375313e455e069da202`, after #963. Replace one
range over `strings.Split` with `strings.SplitSeq`. The module already
requires Go 1.27.3. No new dependency, budget, syntax, acquisition,
source identity, or user decision. R53 and R5 remain partial.

The helper still checks NUL and absolute paths first, normalizes
backslashes, checks the cleaned path for escape, then rejects empty,
dot, or parent segments in the authored normalized spelling. Do not
replace those checks with a cleaned-path-only admission rule.

## Reachability And Preservation

The only direct caller of `validateV4Subpath` is the Git branch of
`parseV4Source`, after ref/origin validation and before
`NormalizeRegistryPath`. Both public byte parsing and actual config-file
loading reach it. Directory and Marketplace path-field refusals are
unchanged. Parsing does not acquire a repository.

Independent direct examples cover accepted paths, raw non-UTF-8 bytes,
deep paths, NUL/absolute priority, backslash traversal, cleaned escape,
empty segments, dot segments, and trailing separators. Freeze the former
helper byte-for-byte after its test-only rename as a raw-byte fuzz
oracle. Shared standard-library path routines are unchanged.

Twenty public cases exercise both `ParseV4Config` and
`LoadV4ConfigFromPath`. Success checks normalized Git subpaths, source
binding, and the selected skill. Refusal uses `errors.As` and checks the
typed `v4.git-path` code, `sources.tools.path`, file, line, and message.
The message assertion specifically preserves refusal precedence; it
does not substitute for a typed-code assertion. Both consumers preserve
actual input bytes, mode, mtime, and directory contents.

All five semantic controls fail through both public consumers and the
independently authored direct examples, without compilation failures:

| Mutation | Observable contract |
| --- | --- |
| Omit segment checks | Dot, parent, and empty segments remain rejected |
| Omit cleaned-escape guard | Escape refusal retains priority |
| Omit backslash normalization | Backslash traversal is not admitted |
| Omit NUL/absolute guard | Early refusal retains priority |
| Ignore trailing empty segment | A trailing slash is not admitted |

Native fuzzing passes on both variants: 367,770 before executions and
359,104 after executions in separate 15-second, four-worker runs.
Focused after-variant race tests pass. No source input limit is added.

## Measurement

Ten direct shapes and nine public cases run in six alternating
one-second pairs: 228 retained observations. The trial sequence is
`before, after, after, before` repeated three times. Fixtures and
benchmark definitions are frozen before the first actual baseline
trial and remain identical between variants. A separate one-iteration
fixture smoke and allocation profile are not evidence rows.

Direct checks have no filesystem or network work. Public cases parse
YAML and normalize 1, 256, or 1,024 Git sources, each binding one skill,
with empty, three-segment, or backslash subpaths. They include Git
URL/ref validation but not acquisition, config reads, or directory
path-metadata operations. Fixture construction and result assertions are
outside `b.Loop`.

### Public Allocation Results

| 1,024 Git sources | Before median allocs | After median allocs | Bytes saved, medians |
| --- | ---: | ---: | ---: |
| Three-segment paths | 57,543 | 56,520 | about 47.9 KiB |
| Backslash paths | 59,591 | 58,565.5 | about 48.1 KiB |

The helper removes exactly one split-slice allocation per nonempty
path reaching segment iteration. Public YAML/pool measurements vary by a few
allocations: observed median reductions are 1,023 and 1,025.5, not
invented exact 1,024-allocation deltas. The useful public claim is
**about 1,024 fewer allocations and 48 KiB saved per call** at this
fixture size. At 256 and one source the same per-source effect remains.
Empty-path public controls retain their allocation counts.

### Timing And Direct Results

No large-fixture public timing comparison is significant: empty-path
parsing is 4.859 to 4.887 ms (p=0.485), three-segment parsing is 5.163
to 5.107 ms (p=0.240), and backslash parsing is 5.238 to 5.238 ms
(p=0.818). The 256-source backslash comparison is nominally 2.01%
faster (p=0.026), a weak isolated signal across the matrix. No
public-path latency claim.

Direct ordinary slash-separated paths become allocation-free:
single-segment checks are 31.71 to 21.96 ns and three-segment checks
are 61.10 to 44.40 ns (both p=0.002). Backslash checks are 93.97 to
78.45 ns (p=0.002), retaining one normalization allocation instead of
two total allocations. A 64-segment valid path removes its 1,024-byte
slice allocation; its timing difference is nonsignificant (p=0.069).
Refusals reaching segment iteration also remove the split allocation.

The unchanged early absolute guard is nominally 3.08% faster, only
0.263 ns (p=0.009); that branch returns before iteration, so do not
attribute this signal to the change. Empty and early escape controls
are nonsignificant. Retain all unfavorable point estimates too. Do
not summarize unrelated micro/public shapes with their geomean.

The baseline public nested-path allocation profile attributes 589,851
sampled objects of 25,368,643 (2.33%) to the split call. This identifies
the allocation site, not a public CPU or acquisition-latency claim.

## Reproduction And Provenance

`security-r5-git-subpath-validation-2026-10-27/` retains complete
`before.txt`, `after.txt`, `benchstat.txt`, and `manifest.json`.
The manifest pins the original Git base, native Go version, all
config-package Go inputs and module-file SHA-256 values, frozen test
binary SHA-256 values, exact arguments, case identities, trial order,
and each complete stdout hash. Other dependency source is pinned by
the Git base, not claimed to be enumerated by the config-only hashes.

Compile native Go 1.27.3 Darwin/arm64 binaries with `CGO_ENABLED=0`.
Use the committed test/benchmark definitions for both. Before uses
the base's `v4.go`; after changes only the segment iteration line.
The retained source hashes identify each reconstruction. Invoke each
binary six times according to the alternating sequence:

```sh
config.test -test.run='^$' \
  -test.bench='^Benchmark(V4SubpathValidation|ParseV4GitSubpaths)$' \
  -test.benchtime=1s -test.benchmem -test.count=1
benchstat before.txt after.txt
```

Each invocation must report all nineteen cases, positive iterations,
finite nonnegative values, `ns/op`, `B/op`, `allocs/op`, and `PASS`.
Verify all 228 observations, six samples per variant/case, source
reconstruction, source/binary/trial identities, and byte-exact
benchstat output before publication. Never relabel the measured base,
pool different bases, or compound allocation credit with #963.

## Validation

On the measured base, before and after focused validation passes:
new preservation tests, whole-root vet, zero-issue lint, and build.
Before-overlay preservation, focused after-variant races, both fuzz
runs, five semantic controls, and offline `go mod tidy -diff` pass.

Fresh selected config/pipeline/resolver/TUI validation passes with
vet, zero-issue lint, and build. Strict specs pass with 68 items,
1,284 scenarios, 2,104 annotations, and two active changes. All
85 terminal journeys plus four utilities pass with zero skips.
Combined E2E passes in 758.904 seconds: 3,502 invocations, all 114
requirements, zero gaps, all three required refusal witnesses
executed and passed, plus shellcheck. CI owns the full root suite;
these scoped package results are not a full-suite claim.

Main subsequently adds approved HTTPS-fallback work at `98498eff`.
Its changed production packages are absent from the config test
dependency graph, and no measured config/module/definition inputs
change. Keep the measurements tied to `47be33f6`; refresh affected
consumer gates after integration rather than relabeling old gates.

Host adversarial `open-code-review-delegate` covers all nine own
files, including default-excluded tests/docs/raw data and private
measurement/control/verification harnesses. The initial review accounts
for all 228 observations with zero skipped files and no actionable
findings. Repeat review before push and handoff.
