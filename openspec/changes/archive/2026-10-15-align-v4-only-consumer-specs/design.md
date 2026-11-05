## Scope

Audit all current specs and active spec deltas for consumer-format claims. The `consumer-config-format` capability already defines exact-v4 roots, accepted include fragments, path-specific refusal, no new work on refusal, recovery of already-admitted dependency transactions, and absence of a shipped migrator. Reuse that authority instead of duplicating its refusal matrix in every capability.

## Decisions

- Qualify acquisition-independent reads with format validation. Missing acquisition evidence is not malformed consumer input; refusing an unsupported root must not be weakened into ignoring an unbindable layer.
- Keep Marketplace source names in authored configuration and instance identity in verified acquisition evidence. Alias conflict checks retain their current behavior.
- Replace consumer-facing top-level collection lookup with `resources.<source>.include: [collection/<name>]`. A required group selects its whole Git or directory catalog only with `all: true`; an explicit subset stays a subset, and policy alone selects nothing.
- Keep internal resolver-model controls, but bind the corrected collection scenarios to tests that load authored v4 configuration and to the existing v4 E2E workflow.
- Preserve archived change records. Update active harvest destinations and retired gate instructions so future work cannot reintroduce removed consumer support.

## Verification

Run focused v4 parsing, selection, authored-read, settings, refusal, and startup tests, plus the v4 collection E2E witness. Run `make validate`, strict pinned OpenSpec validation, scenario traceability, and archive checks. Complete an OCR delegation-mode self-review of every changed file before handoff.

## Compatibility

No runtime behavior or persisted format changes. Acquisition-lock v1/v2, historical source revisions, operator and wire formats, native harness data, ownership state, and dependency recovery remain independent supported contracts.

## Verification Results

- Focused parsing, required-selection, authored-read, settings, instance-identity, refusal, installation, and progressive-startup tests passed.
- `make validate` passed the selected config, resolver, and TUI packages, CI lint, and build; this is not a full root-suite claim.
- `make validate-e2e` passed with all 120 command/flag requirements covered, all three required unsupported-config witnesses executed, and shellcheck passing.
- `make tui-qa` passed typechecking, four driver checks, and all 24 real-terminal journeys.
- Post-archive, post-rebase `make spec-check` at `406f847f` passed 53 items, 778 scenario identities with 1328 annotations, and the archive audit.

## Adversarial Review

Reviewed the intended workspace diff against `c33ab79cfb5fcd1e6f2d2de86311222612813364` using OCR delegation-mode file selection and rules. OCR selected both YAML files; its 27 default-excluded Markdown and test files were also inspected manually, including the seven archive-generated canonical specs. Total files: 29; reviewed files: 29; skipped files: 0; coverage: 100%. No actionable findings remained. The review checked consumer-path reachability, unsupported-input refusal, acquisition and write ordering, scope/provenance, independent compatibility contracts, and non-vacuous regression controls. Passing checks and review do not prove the absence of defects.
