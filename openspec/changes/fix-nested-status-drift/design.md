## Context

`sync --check` resolves and previews at the nested local configuration directory when it differs from the repository root. `status` resolves the nearest local configuration but previews at the repository root.

## Goals / Non-Goals

**Goal:** Keep `status` drift and orphan reporting rooted at the output location chosen by synchronization.

**Non-goal:** Change synchronization, ownership, pruning, or resource selection.

## Decisions

Select the materialization root in `status` using the same local-directory rule as `sync --check`, while retaining the repository root for configuration resolution and unrelated status sections. Reusing the synchronization rule is smaller than changing the pipeline or materializer and avoids moving other status output without evidence.

## Risks / Trade-offs

Nested output fixtures must include parent-owned artifacts, or the test could pass while false removals persist. Exercise both the synchronized and missing-output states from the nested directory.
