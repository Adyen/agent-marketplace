# Design

## Context

See proposal.md for motivation and docs/plans/2026-11-13-target-profiles-design.md for the design being extended. This change records proposals, not supported runtime behavior.

## Goals / Non-Goals

Settle place-delivery staging and publication, interruption recovery, root-bound ownership, whole-set conflict planning, edited owned files and failed verification by reusing existing filesystem boundary, materialization, dependency-transaction, ownership and package-generation seams. Do not change production code, journals, ownership state or normative specs. Do not design automatic adoption or relocation, register-mode command execution, rollback of external host registration or a new parallel ownership store.

## Decisions

Record the selected approach, transition table, observation limits and future witnesses in the existing target-profile design rather than duplicating them here, so one document stays authoritative until the place-delivery change makes it normative.

## Risks / Trade-offs

A proposal can read as implemented behavior or overstate atomicity, hot-reload safety or recovery guarantees. Label every proposed outcome as proposed, check each cited seam against current code and specs, and state what the filesystem and host cannot guarantee.
