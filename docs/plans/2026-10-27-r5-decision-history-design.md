# R5 / R53: Bulk Decision History Copying

## Scope And Choice

Independent partial R53/F136 work from main `85bc15d7`. PR #967 remains
open and is not included. Request rebuilding copies every unchanged
selection decision separately while interpreting its source group.
Copy each already-ordered group with one standard-library append before
the existing interpretation loop. Count decisions during the existing
source-name pass and allocate exact history capacity only when nonzero.

Reject the initially measured two-line relocation alone: its chunked
growth allocates 10.75% more bytes with sixteen groups. Exact capacity
removes that regression without another pass, cached state, or changes
to the selection interpreter. Preserve the rejected dataset separately.
No new source admission, limit, syntax, dependency, or user decision.
R53, R5, and #765 remain partial.

## Preservation

Keep source-group lexical order and every decision's intra-group order,
including repeated includes, exclusions, provenance, nil requests, and
private non-selection actions. Reset previous output on each rebuild;
preserve nil output for nil or empty streams. Decision records remain
copied values while their request pointers retain original identity.
Do not alias input/output decision slice storage in either direction.

Reuse the existing independently authored per-locator interpreter and
Rapid selection/history property, plus include-tree consumer tests.
Add focused ownership/reset and public parsing/actual-file-loading
examples with repeated locators and required exclusions. Preserve source
binding, aliases, history file/line/precedence, chosen requests, and
authored file bytes/mode/mtime/directory contents.

## Measurement

Freeze benchmarks before modifying production. Eight direct rebuilding
shapes include empty/single inputs, 256/1,024 includes or exclusions,
sixteen groups, and a 1,024-singleton-group case. The latter controlled
the rejected bulk-only change but is affected by exact preallocation.
Two unaffected
effective-request cases control the sibling interpreter. Six public
cases parse 1/256/1,024 include-only Git or exclude-only Marketplace
decisions without acquisition or config/directory path I/O.

Compile separate native Go 1.27.3 binaries, execute an actual baseline
trial before the production edit, then complete six alternating
one-second pairs over all sixteen cases: 192 observations per candidate,
384 across separate initial exact-capacity and rejected datasets.
Blocking CI then requires the #972 x/net security repair and supported
Ubuntu 24.04 browser runners. Preserve those initial matrices and repeat
all sixteen cases with the fixed module/workspace inputs on both sides:
another 192 observations, 576 total across three separate stages.
Record original v4 code from `85bc15d7` and dependency snapshot
`95320ec46b58f87b361d34f3fd6d015eed848860` explicitly; do not pretend
old module fingerprints describe the new binaries. Keep setup
and full result assertions outside `b.Loop`. Profile baseline allocation
sites; retain source/binary/trial hashes and byte-exact benchstat output.
Never pool variants/bases, compound earlier allocation savings, or hide
controls. Report the final empty helper's measured 0.30 ns increase alongside
its unchanged zero allocations and nonsignificant single-item public
timings; do not infer an include-only public speed-up from noisy timing.

## Verification And Publication

Run both variants' preservation tests and the existing Rapid property at
1,000 cases. Use compiled semantic mutants to prove history/order/
ownership assertions and distinguish public witnesses from helper-only
contracts. Run focused races, offline module hygiene, selected validation,
strict specs, terminal QA, and combined E2E with required refusal witnesses.

Host adversarial `open-code-review-delegate` covers all own files,
including default-excluded tests/docs/raw data and private harnesses,
before every push and handoff. Follow actionable CodeRabbit/Gemini
feedback and CI. Leave the clean PR open, unqueued, with auto-merge off.
