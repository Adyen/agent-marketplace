## Context

The curated tool table translates canonical identifiers; it is not an inventory of every runtime's built-ins or dynamically registered MCP tools. Factory ingestion merges `Edit`/`Create` pairs, Cursor ingestion reverses known mappings, and Gemini ingestion keeps native identifiers in the canonical field. The Factory, Cursor, and Gemini compilers apply the canonical table even when the source used their own layout, so valid native identifiers are rejected and the subagent is suppressed. Claude and Devin same-harness output already keeps its lists.

## Decisions

Use typed native sidecars to retain source tool lists. Factory and Gemini gain tool-list fields; Cursor ingestion populates its existing field before reversing identifiers. A shared resolver emits a native list exactly when the compiler's own sidecar holds one, and otherwise applies the curated mapping with each compiler's existing diagnostic code, message, and severity. A sidecar is read only by its own harness's compiler, so a native list never reaches another target.

Preserve nil versus explicit empty, keep native order and exact identifiers, and do not infer native provenance from filenames or opaque extensions. Explicit invocation tool overrides apply to native identifiers as well. `disallowedTools` and `isolation` checks are unchanged.

Claude and Devin subagent tool handling, diagnostic severities, and materialization pruning are outside this change.

## Alternatives

Expanding the Factory table fixes only today's built-ins and cannot enumerate registered MCP tools. Passing unknown tools through to every target confuses namespaces and can broaden permissions. Bypassing compilation for same-harness files loses typed-field validation of other metadata.

## Validation

Public API matrices cover same-harness native policies across all seven layouts, empty and absent lists, supported cross-harness mappings, explicit invocation overrides, and refusal of foreign identifiers by the Factory, Cursor, Gemini, and Codex compilers. CLI regressions cover Factory conversion and native install/sync in repository and user scope with symlink and file materialization. Benchmark tool-policy compilation and native conversion before and after.

Native reader delivery, writer exclusion, discovery, and resumed-session enforcement require independent vendor runtime probes. File and configuration checks are not runtime enforcement evidence.

### Verification evidence

Against the unchanged base `4fcbb096`, the native-policy matrix fails exactly for `factory-to-factory`, `cursor-to-cursor`, and `gemini-to-gemini`; every other asserted source/target cell, the empty/absent cases, the supported-mapping matrix, and the override test already pass there, so the asserted cross-harness cells keep their base behavior. With this change all pass. The CLI suite passes 20 native install/sync cases across five harnesses, two scopes, and two materialization modes, plus the Factory `convert` reproduction from #599.

Five two-second benchmark samples per side on one macOS/arm64 host exclude disk and network I/O. Pure compilation allocations are unchanged for every target. Native Factory conversion adds one allocation (339 to 340) and Cursor adds two (325 to 327) to retain the raw list. Timing shifts of at most 3.5% also appear on Claude compilation, whose code is unchanged, so they are within run-to-run drift; the geomean moves +1.5%.

Adversarial review covered the full diff against `4fcbb096`: sidecar provenance and cross-target leakage, nil/empty preservation through ingestion and JSON-tagged sidecars, aliasing of ingested slices, override application, consumer reachability through `sync` and `convert`, and the existing fail-closed regressions for foreign identifiers. No correctness or security findings were confirmed.

`make validate`, `make validate-e2e`, focused consumer packages (`internal/materialize`, `catalog`, `pipeline`, `sourcerevision`, `marketplace`, `resolver`), and pre-archive `make spec-check` passed.

The verified implementation is committed as `5b137a66`. Post-archive `make spec-check` passed: strict spec validation, scenario/test traceability, and the archive-before-merge guard.
