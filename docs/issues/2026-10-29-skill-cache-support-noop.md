# Skill Cache Support No-Op (#976)

Base: main `dd85ebcb`. Apple M4 Max, macOS, Go 1.27.3, `CGO_ENABLED=0`.
The host runs endpoint-security software, and the load average was 12-20
during measurement. Raw data: [skill-cache-support-noop-2026-10-29](skill-cache-support-noop-2026-10-29/).

## Problem

For each cache-backed skill, every real sync copied the source support tree
into a stage, deleted every cached support file, and copied the stage back.
The existing byte/mode no-op in `writeSupportSnapshot` therefore never
applied. Every unchanged sync gave each support file a new inode and mtime.

## Change

Before staging, `prepareSkillLinkCache` compares two existing exact-tree
digests:

- the cache's actual digest, in the `portablePathDigest` format but read
  through the admitted cache-namespace descriptor;
- the desired digest that `sync --check` already uses
  (`portableSkillVirtualDigest`).

Both digests cover compiled artifacts, support bytes and modes, directories
and any other entry. Preparation returns without staging, pruning or writing
only when all of these hold:

- the digests match;
- the same captured walk copied every declared support file;
- the namespace path still denotes the admitted parent;
- the cache entry is still the compared directory in the same state.

Any other outcome runs the unchanged full preparation, with its diagnostics:

- a mismatch;
- a missing or unreadable cache;
- a refused support source (the desired digest uses the same captured
  source reader);
- a missing declared support file;
- a changed namespace.

A missing cache fails the first digest before any source support is read.

## Workflow Results (Real Binary)

Same driver as the #765 investigation, using `workflow.py`. The driver
creates an isolated sandbox with a local bare `file://` registry, one locked
Git root and Factory symlink mode. Values are medians of 7 warm runs after
2 warm-ups. Each run performs one source-revision build and leaves the lock
unchanged.

| Shape | `sync` before | `sync` after | `sync --check` before | `sync --check` after |
| --- | ---: | ---: | ---: | ---: |
| 330 skills, no support | 962 ms | 732 ms | 639 ms | 623 ms |
| 330 x 2 x 15 KiB (about the largest observed) | 3,805 ms | 1,429 ms | 1,374 ms | 1,268 ms |
| 330 x 8 x 16 KiB | 10,306 ms | 2,837 ms | 2,684 ms | 2,722 ms |

`sync --check` is the unaffected control. With the new binary, a warm
unchanged `sync` on the 660-file shape changed none of 1,650 cache entries
and installed links. The previous materialization code, measured on `b5f73185`
during the #765 investigation, replaced all 660 support files.

Positive control: after an upstream commit edited one support file, `update`
and `sync` installed the new content. `sync --check` then exited 0
(`identity-after.txt`).

## Benchmarks

`BenchmarkRunUnchangedSkillLinkCaches` and
`BenchmarkRunChangedSkillLinkCaches` run real `materialize.Run` passes with
disk I/O and no network. The runs are interleaved, with 10 samples per side;
fixture changes happen outside the timer.

| Case (64 skills) | Before | After | Change |
| --- | ---: | ---: | ---: |
| Unchanged, no support | 163.7 ms | 84.0 ms | -48.7% |
| Unchanged, 2 x 15 KiB | 299.9 ms | 140.2 ms | -53.2% |
| Unchanged, 8 x 16 KiB | 721.5 ms | 320.3 ms | -55.6% |
| 1 skill changed | 305.2 ms | 139.9 ms | -54.2% |
| All 64 skills changed (same-size edits) | 305.0 ms | 436.6 ms | +43.2% |
| Caches missing | 309.8 ms | 315.5 ms | ~ (p=0.579) |

All results are p=0.000 except the missing-cache case. Allocated bytes fall
by about 34% in the unchanged cases.

The worst case is a sync after every cached skill changed with same-size
content. It pays for both digests before full preparation: +43% time and +47%
allocated bytes. Like `sync --check`, the desired digest holds one skill's
support bytes in memory at a time.

## Review Follow-Ups

Two review fixes harden the fast path after these measurements were taken:

- `907ced8e`: declared support must come from the captured walk.
- The following commit: the decision is bound to the compared cache
  directory's identity and state.

Both add only per-skill bookkeeping. Sanity runs of
`BenchmarkRunUnchangedSkillLinkCaches` afterwards measured 76-77 ms,
124-128 ms and 291-295 ms, matching or improving on the recorded after-change
medians. The tables above were not re-measured.

## Limits

- One host; Linux was not measured.
- Only page-cache-warm runs were measured.
- No new limit, cross-invocation cache or trust marker is added. The
  comparison runs on every real sync.
