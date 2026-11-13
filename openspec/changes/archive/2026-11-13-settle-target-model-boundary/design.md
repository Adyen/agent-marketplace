# Design

## Context

See proposal.md for motivation and docs/plans/2026-11-13-target-profiles-design.md for the design being clarified. This change records recommendations, not supported runtime behavior.

## Goals / Non-Goals

Clarify the application target authority, current accepted-kind and delivery matrix, glossary boundaries, and the next bounded refactor. Do not change production code, normative specs, consumer configuration, locks, or outputs. Do not implement profiles or freeze their schema.

## Decisions

Keep the application-wide target model in internal/harness, composing compiled translation declarations and existing installation adapters. Keep distribution, profile loading, consent, and concrete root resolution outside pkg/harness-compilation. Record the full rationale and implementation brief in the existing target-profile design rather than duplicate it here.

## Risks / Trade-offs

A design matrix can overstate support when a compiler emits only a diagnostic or plugin delivery requires a separate package path. Check each distinction against current code, catalogs, specs, and regression tests.
