## Context

See proposal.md for the motivation. Configuration intent still discovers its own roots while the pipeline has a Workspace value that validates both roots and ties materialization to the personal-local directory. `internal/config` cannot depend on `internal/pipeline`.

## Goals / Non-Goals

The implementation keeps one selected root pair through each read and rejects invalid roots before the read. It does not change Workspace containment rules, the config schema, or lock acquisition policy.

## Decisions

- Construct a Workspace at each CLI or pipeline entry that has roots or needs discovery; pass `RepoRoot()` and `LocalConfigDir()` into the string-typed configuration options. This keeps the package dependency direction intact. Passing independent finder results would retain the validation bypass.
- Reject an empty repository root at the start of configuration intent loading. Do not derive a root from cwd there; discovery remains in `DiscoverWorkspace` or the established explicit-root constructor.
- Reuse existing command fixtures where possible and test the observable nested and escaping paths. An isolated config test proves the missing-root error without relying on command setup.

## Risks / Trade-offs

- Valid callers relying on an implicit local root may expose hidden assumptions. Audit every production caller and run both focused and full regressions.
- Symlinked nested roots can be retargeted after construction. Recheck existing Workspace validation at the consumer boundary; do not introduce a second containment policy.
