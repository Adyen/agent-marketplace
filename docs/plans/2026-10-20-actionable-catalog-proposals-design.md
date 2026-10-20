# Actionable model catalog proposals

The nightly audit and its proposal job have separate responsibilities. The audit
reports inventory differences, documentation changes, and unknown evidence. A
proposal creates catalog stubs only for missing IDs with unambiguous provider
mappings; it must not turn a changed evidence fingerprint into a hash-only PR.

Keep the existing audit failures, summaries, and JSON artifacts. Report changed
evidence hashes and manual-entry candidates without changing reviewed baselines.
Even a proposal with real catalog additions leaves baseline hashes and checked
dates untouched so unrelated documentation is not implicitly acknowledged.

Gate publication on changes in the model catalogs, rather than on any dirty
file, and stage only catalog changes. Preserve the human-push check and leased
branch publication. A no-change run does not edit or close an existing PR.

Alternatives rejected: silently refreshing hashes would acknowledge unreviewed
evidence; only adding a workflow guard would leave the standalone proposal
command rewriting reviewed data. Semantic parsers for every hook and tool
document are outside this focused fix.

Prove the policy through proposal tests and the actual workflow shell block,
using a built proposal tool, temporary Git repositories, and a recording GitHub
stub. Cover documentation-only drift, ambiguous model candidates, a defensive
baseline-only edit, actual catalog additions, and baseline byte/mtime
preservation. No vendor calls or real PR writes occur in these tests.
