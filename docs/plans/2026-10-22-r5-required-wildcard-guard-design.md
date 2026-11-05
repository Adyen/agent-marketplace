# R5 / R53: Skip Wildcard Matching For Protected Requests

Start independently from main `81c6a6e2`. PR #900 is clean and remains open;
this slice must not depend on its branch or silently count its wins twice.

`rebuildRequests` tests the wildcard matcher before `request.Required`, even
though required selections cannot be excluded. Its sibling interpreter
`effectiveV4Requests` already tests the required flag first. Reorder the one
condition in rebuilding, leaving the common matcher and all policies intact.
The matcher is pure and total; skipping it for protected requests changes no
result, diagnostic, metadata, selection order, refusal, or write behavior.

Retaining the current order is the alternative if measurements do not justify
the change. Do not introduce an index, cache, new budget, selection truncation,
dependency, deadline, graph rule, or accepted-input change.

Use valid Marketplace documents with N distinct registry-qualified skills,
N distinct registry/kind wildcard exclusions, and a retained command. Measure
required and optional groups separately at N=1,256,1024, both private
rebuilding and public parsing. Same-document duplicate exclusions are refused;
an exploratory duplicate-list fixture is invalid, not benchmark evidence and
not a reason to relax that refusal.

Measure two separate interactions with identical benchmark definitions:
main's existing matcher, then PR #900's allocation-free matcher. Preserve both
datasets, use canonical physical overlays and immutable binaries, verify
different variants before collection, and alternate six two-second pairs.
Do not pool runs, add overlapping allocation benefits, claim a global CLI
speed-up, or call the still-quadratic protected scan bounded.

Keep all fixture setup and checks outside timing. Preserve required/optional
replacement, global/kind/registry selectors, order/provenance, input
immutability, and healthy command behavior through deterministic and existing
independent generated controls on both variants. Use semantic policy mutants,
public benchmark guards, focused races, and the config consumer gates.

Run selected validation, specs, combined E2E, and terminal checks. Host
`open-code-review-delegate` reviews every file, including raw evidence, before
every push/handoff. Follow actionable CodeRabbit/Gemini feedback and CI; ignore
major-release commentary and leave the clean PR open, auto-merge disabled.
R53 and R5 remain partial.
