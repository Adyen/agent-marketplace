## Context

Claude source-layout ingestion lists every immediate subdirectory of `skills/` as a skill, including `skills/.git`; the bare skills-directory form already filters hidden directory names. Catalog-level recursive scanning skips `.git` at bundle boundaries but cannot undo diagnostics emitted during ingestion.

## Goals / Non-Goals

Exclude `skills/.git` from skill candidates before parsing, without suppressing diagnostics for malformed authored skills. Do not change directory-source configuration, materialization, or source-root security validation.

## Decisions

Filter only `.git` at Claude's skill-directory enumeration boundary. This keeps the existing traversal and catalog diagnostic semantics while removing the false candidate at its origin. Filtering all hidden directories or discarding malformed diagnostics later could hide authored problems, and moving the filter into catalog discovery would not protect direct ingestion.

## Risks / Trade-offs

A source that intentionally names an authored skill `.git` will no longer discover it; this path is reserved for Git metadata. A local-source consumer regression with a real `.git` tree and a malformed authored control proves that legitimate warnings are retained.
