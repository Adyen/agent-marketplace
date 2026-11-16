# Design

## Context

See proposal.md for motivation and docs/plans/2026-11-13-target-profiles-design.md for the design being extended. This change records proposals, not supported runtime behavior.

## Goals / Non-Goals

Settle profile identity, discovery and kind encoding, the authenticated profile binding for host-package assessments, and the disclosure policy for host-package analysis. Do not change production code, normative specs, wire formats, locks or outputs. Do not design a permission sandbox, register lifecycle or garbage collection.

## Decisions

Record the proposed shapes, refusal cases and trace table in the existing target-profile design rather than duplicating them here, so one document stays authoritative for the direction until the place-delivery change makes it normative.

## Risks / Trade-offs

A proposal can read as implemented behavior, or overstate what static analysis or a disclosure guarantees. Label every proposed field and outcome as proposed, check each cited seam against current code and specs, and state the limits of analysis explicitly.
