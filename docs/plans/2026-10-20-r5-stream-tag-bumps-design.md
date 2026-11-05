# R5 R10: Stream Git Tag Bump Selection

## Scope And Choice

Start from local `origin/main` at `99c26d07`. R10 (A47/F31) is on the
performance track. The supported consumer is `update --bump`, through
`bumpRepoRegistryRefs` and `discoverTagBump`. Git supplies the tag names;
the existing implementation buffers all stdout, converts it to a string,
and splits that string before choosing one ref.

Keeping that implementation retains proportional but unnecessary memory.
Sorting/filtering tags differently in Git could change numeric overflow,
family matching, and configured ordering. Prefer scanning the existing
`git tag -l` stream and retaining only the best candidate and current line.
Use the standard library, not a new dependency or persistent cache.

There is no new tag count/byte cap, deadline, graph policy, or truncation.
Do not change acquisition, contexts, config/lock staging, source ownership,
or materialization. D2 and D5 decisions remain separate. This is a
semantics-preserving optimization, with `Spec-Impact: none`.

## Preservation And Evidence

Keep trailing-integer parsing, whitespace around the authored ref, literal
prefix matching, rejection of nonnumeric suffixes, integer overflow errors,
lexicographic ties, equal-current results, and no partial result on failure.
Git failure must still win over a candidate/parse result. Do not introduce
Scanner's default 64 KiB token ceiling for otherwise accepted long tags.
Retain `Output`'s typed `ExitError.Stderr` payload: its existing 32 KiB
prefix/tail capture and omission marker, without unbounded stderr buffering
or a new public diagnostic format.

Exercise real local Git repositories in preservation tests at different
configured tag orders. Retain a multi-size benchmark including process and
local packed-ref I/O, excluding network acquisition and subsequent writes.
Capture five two-second before/after time and allocation samples. Add
adversarial stream/failure controls alongside the implementation.

Run affected pipeline and CLI update tests, root validation, strict specs,
and the combined E2E/configuration gate. Review every changed file through
`open-code-review-delegate`, including its default exclusions. Frozen R5
artifacts remain unchanged; this slice does not close the security track.
