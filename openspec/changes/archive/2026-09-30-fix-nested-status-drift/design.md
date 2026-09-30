## Context

`sync --check` and `status` both inspect through `pipeline.Inspect` on a `pipeline.Workspace` whose materialization root is the nested local configuration directory when present, so previewing matches reconciliation. That nested parity contract is still unspecified and untested.

## Goals / Non-Goals

**Goal:** Specify and regression-test `status` drift and orphan reporting rooted at the output location chosen by synchronization.

**Non-goal:** Change synchronization, ownership, pruning, resource selection, or any production code; the upstream workspace inspection already implements the behavior.

## Decisions

Cover the existing `pipeline.Inspect`/`Workspace.MaterializeRoot()` path with a spec delta and command tests rather than new production code. Reusing the shared workspace inspection keeps `status` aligned with `sync --check` by construction, so this pull request is regression-and-specification-only.

## Risks / Trade-offs

Nested output fixtures must include parent-owned artifacts, or the test could pass while false removals persist. Exercise both the synchronized and missing-output states from the nested directory.
