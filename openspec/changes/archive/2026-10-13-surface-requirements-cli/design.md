## Context

The existing resolver completes transitive expansion after collections. RequiredBy records both implied admission and satisfied direct providers, while Via remains collection-only. Pipeline ResolveResult already carries disabled ImpliedPreview, but InstallResult exposes only filtered warnings. Configured list uses ResolveCatalogView, status uses resolved artifacts and inspection, and doctor uses cache-only resolution.

## Goals / Non-Goals

Goals are the delta-spec reporting behaviors with no selection or acquisition change. Non-goals are new commands/flags, expansion-policy or default changes, wire formats, Marketplace graph, materialization/ownership policy, TUI and web changes.

## Decisions

- Add a reporting-only Implied boolean to ResolvedArtifact, set only when impliedArtifact admits an expansion candidate. Keep direct RequiredBy edges distinct and preserve Via. No policy uses this flag.
- Extend InstallResult with AlsoSelected and ImpliedPreview slices. Add one complete ResolveResult capture option alongside the existing resolved capture in silent_sync.go. Every install route captures its existing sync result; no extra resolution, catalog load or network call is introduced.
- Filter summary artifacts by a visited kind/name closure rooted in that install's existing resourceIdentity set, using RequiredBy edges. Traverse all selected and preview artifacts so direct intermediates can lead to transitive implied resources, but emit only Implied artifacts or preview candidates. Preserve deterministic artifact order, trim summary RequiredBy to reachable requirers, and deduplicate scoped identity so repeated request/dependent edges do not duplicate summaries.
- Share requirement-provenance formatting between list and status in a focused requirement_output.go. Keep collection Via intact and append canonical requires labels; escape at existing rendering boundaries. Ad-hoc declarations render kind/name only, so raw source hints are not printed.
- Add Reason to requirementRow and print it before FIX. Doctor annotates only existing typed source-not-configured warnings, using warning.Remediation and existing terminal protection rather than reinterpreting source identity or fetching sources.
- Test through real sandboxed command entry points, reusing existing fixture helpers; add provenance-only resolver/pipeline tests when their reporting seam cannot be reached sufficiently through commands. No command isolation runner or shared verification infrastructure changes.

## Risks / Trade-offs

A direct provider with RequiredBy must not be described as implied. Kind/name satisfaction is global but admission has scope, so output must retain scope-specific identity rather than invent new policy. Preview traversal must not turn into a write path. Source hints are untrusted and credential redaction must precede literal formatting. Existing list/status benchmarks provide before/after evidence; no acceptance threshold is introduced.

## Migration Plan

No migration or compatibility-format change. Preserve the release-N expansion default false.

## Open Questions

None. RequiredBy semantics, existing capture and doctor cache-only behavior were inspected in the governing source before this design.
