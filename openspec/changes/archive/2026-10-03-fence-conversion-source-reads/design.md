## Context

Direct runConvert passes raw disk DirFS to hcc.Autodetect and hcc.Convert independently of catalog fencing. The real Cobra regression imports a foreign linked skill and overwrites prior native conversion output with no diagnostic/error.

## Decisions

1. Capture one SourceFS after explicit flag validation and reuse it for detection and primary ingestion. Payload reads remain regular single-link/nonblocking stable snapshots with captured-size streaming/growth checks.
2. Inspect the view's typed first refusal after detection and conversion; fail before output staging/publication rather than treating it as ordinary lossy syntax and copying valid/invalid output. Normal syntax partial-result semantics stay intact.
3. Retain existing source/output overlap and canonical root generation checks, user-facing summary, supported flags and contained file aliases. Conditional cleanup close errors propagate without replacing primary failures.
4. Remaining support copying and conversion output publication/pruning are separate R2 consumers; no whole-tree source snapshot or new arbitrary input budget.

## Verification

Actual isolated Cobra foreign skill source after positive conversion was red/green: parent reports successful overwrite; corrected source refusal retains ErrUnsafePath and installed/source/external generations. Contained relative file alias and hard-link typed refusal/no new output-root controls verify the consumer. All existing explicit/autodetect/flag/overlap/partial/native conversion tests and command-isolation guard pass on Linux/macOS, as do count20 selected races, scoped validate/lint/build and strict specs. The initial make filter used shell metacharacters that the recursive test target rejected; rerunning the equivalent TestConvert prefix through make passed without changing runner policy. All nine implementation entries including test/benchmark/script/spec exclusions reviewed with no skips, no source comments changed. Final R2 integrated/comment gates remain pending. Four-skill disk primary read/compile dry-run(no output writes/network, logs discarded),2s ×5 arm64 median439.479→2107.937µs(+379.64%),B/op124672→148915(+19.45%),alloc1185→1647(+38.99%),p=.008 n5/no95%interval. Validation cost, not a speedup or full publication benchmark.
