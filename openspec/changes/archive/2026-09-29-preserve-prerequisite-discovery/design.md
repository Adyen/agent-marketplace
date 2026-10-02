## Context

A catalog now appends resource-attached `catalog.requires.invalid` warnings. Sub-bundle discovery asks whether an apparent child directory is already covered by the parent's entries. That coverage check also treats any warning diagnostic (except a fixed list of unconsumed-config codes) as evidence of a discovery boundary. A prerequisite warning is unrelated to that boundary, so a covered organizational grouping gets a false `catalog.discover.sub-bundles-ignored` result.

## Goals / Non-Goals

**Goal:** Keep prerequisite diagnostics visible without changing discovery coverage.

**Non-Goals:** Altering the existing handling of ingestion warnings that really mean sub-bundle content might be missed, or changing selection and materialization.

## Decisions

Add the prerequisite diagnostic code to the existing non-boundary warning list in the discovery classifier. Use the existing covered-sub-bundle regression fixture: declare one malformed prerequisite within a grouped skill and assert both that the coded warning appears and that no ignored-sub-bundle finding appears, while all resources remain discovered. Change only this warning code; treating every resource-attached warning as non-boundary might alter unrelated ingestion behavior.

## Risks / Trade-offs

- [Silencing a real discovery boundary.] → Exclude only `catalog.requires.invalid` and preserve the existing ignored-sub-bundles test for genuinely mixed root resources.
