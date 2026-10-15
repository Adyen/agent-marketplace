## Context

See `proposal.md` for scope and motivation. Current runtime and governing specifications supply the authoring facts; prose-only edits must not change them.

## Goals / Non-Goals

Record each page's reader, quadrant and source checks in private audit evidence. Keep exhaustive syntax and architecture out of task instructions. Do not expand outside-page edits beyond required links.

## Decisions

- Retain the six page URLs by default. Their tasks are distinct; restructure only if the page audit demonstrates a need.
- Pair commands with exact or labelled typical results, including silent success. Distinguish discovery checks from expansion, selection and compilation checks.
- Verify practical commands with isolated fixtures through `go run`. Build with `make build` but do not run compiled Go programs. A sandbox HOME alone does not isolate organization configuration or telemetry.
- Keep existing specifications unchanged and mark this change `skip_specs: true`. Documentation corrects its claims when current source or normative requirements contradict them.

## Risks / Trade-offs

- Printed summaries depend on selected resources, targets and terminal styling. Label variable results and retain source references rather than presenting fixture counts as universal.
- Registry publish steps have external effects. Validate their syntax and local Git steps without pushing example registries or tags.
- A reference-only documentation test does not prove author-guide correctness. Keep the per-page source audit and independent reviews alongside build and contract checks.

## Migration Plan

Publish the guides through the normal site build. No runtime migration or schema change is needed.
