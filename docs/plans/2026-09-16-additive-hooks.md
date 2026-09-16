# Additive hooks — issue #11

Issue: https://github.com/Adyen/agent-marketplace/issues/11

## Agreed direction

Compatibility-first: preserve distinct selected hooks through ingestion, catalog identity, resolution, and materialization without broadly renaming existing resources. Existing names remain registry-local selectors. Other resource kinds retain their collision rules.

Current main already assigns numeric suffixes to repeated event/type hooks in the shared loader. Existing Factory SessionStart catalog coverage and additive materialization tests pass. Remaining gaps include cross-registry winner selection, the independent install-all collision check, Windsurf aggregate ingestion overwriting handlers by name, and Windsurf compilation emitting one ordinary file per event.

## Behavior contract

- Distinct selected hook contributions from independent registries are additive, including identical event/type and matcher with different commands.
- Repeated references to the same physical source still deduplicate. Preserve required policy, config precedence for competing versions of one source, machine/repo separation, and target narrowing.
- Event, matcher, or command text alone is not sufficient duplicate identity. Preserve existing exact-duplicate aggregate semantics rather than introducing blanket content deduplication.
- Existing named installation behavior remains scoped; selecting one resource must not silently install every provider. Install-all must include all applicable hook providers.
- Removing or excluding one registry's contribution must preserve other selected contributions. Collections, list, status, and TUI must retain provider identity.
- Keep generated output deterministic and repeat sync byte-stable. Preserve handwritten entries and fail-closed ownership boundaries.
- Support scripts must remain bound to the correct source; same-basename conflicts must not silently select another registry's code.
- Unsupported target output requires a stable diagnostic, never silent loss or an invented native format.

## Implementation sequence

Use vertical red/green slices, not a batch of speculative tests followed by implementation.

### 1. Cross-registry resolution and installation

1. Add a regression using real Factory hook source files and the public catalog/resolver/materializer path, proving two registries' same-named hooks survive.
2. Centralize hook additivity in resolution while retaining same-source deduplication and competing-ref precedence. Leave non-hook winner selection unchanged.
3. Align install-all discovery with the shared policy and preserve per-registry selections.
4. Cover explicit selection, --from, collections, excludes, removal, and provider-aware catalog/TUI views.
5. Add declarative CLI E2E coverage for the original reproduction and repeat sync/check.

### 2. Remaining ingestion and target compatibility

1. Audit all seven ingesters and relevant output compilers for name-keyed or event-path loss.
2. Repair Windsurf aggregate handler identity before the name-keyed map. Retain existing names where unique and preserve the legacy selector's existing binding rather than repointing it.
3. Verify Windsurf's supported native additive representation before changing its output; unique IR names do not solve the existing per-event output path collision.
4. Any changed interpreted source inventory must use a new source-revision generator contract with independent compatibility vectors and legacy verification. Do not rewrite existing v1/v2 vectors, silently relock supported old snapshots, or migrate organization locks on consumers' behalf.
5. Exercise target output, support-file binding, ownership preservation, and removal in both scopes and materialization modes.

## Regression matrix

- Factory PreToolUse: different matchers, same matcher/different commands, multiple handlers in one group.
- Factory SessionStart: multiple handlers without matchers (required historical regression).
- Same event/type/name across independent registries.
- Repeated references and overlapping paths to one source; competing refs; independent subpaths.
- Required versus optional policy, repo/user separation, narrowed target sets.
- Windsurf aggregate input with repeated handlers, native output with the same event, and legacy-name binding.
- Existing single-hook names, includes/excludes, collections, and supported source-revision manifests.
- Install-all, explicit provider installation, one-provider removal, list/status/TUI identity, repeat sync and sync --check.
- Handwritten aggregate preservation, exact-duplicate semantics, malformed siblings, and support scripts with colliding basenames.
- Non-hook name collisions as a control proving their behavior remains unchanged.

Assertions must inspect complete relevant payloads/counts and forbidden side effects. Materializer-only tests using pre-distinct names are not sufficient coverage for ingestion/resolution bugs.

## Verification

Run narrow package/test selectors for each slice. Capture before/after benchmarks for modified ingestion/resolution hot paths with independently specified fixtures, 2-second benchmark time, five repetitions, and allocation reporting.

Final gates:

- make test
- make ci-lint
- make build
- make e2e
- make harness-compat-check
- make harness-compat-config

Use the documented sandbox for E2E. Native executables run only through the established CI compatibility workflow; configuration checks do not establish native hook execution. Update existing selection/collision documentation with the hook exception and any versioned ingestion compatibility behavior.

Both work packages are required before closing the canonical issue. No new flags, broad name redesign, general ownership migration, or new dependency is planned. Do not commit or push without user authorization.

## First implementation checkpoint

Completed in the first implementation checkpoint: cross-registry resolution, catalog provider visibility, collection expansion and reconciliation, `install --all`/`--from` and single-provider removal, with unit, integration, and declarative E2E coverage.

Still pending: the remaining ingester audit and Windsurf unique aliases, native aggregate output with old-output preservation, source-revision compatibility, support-file basename collisions, full target/scope/mode verification, and real-terminal verification. Issue #11 remains open.

Resolver benchmark on Apple M4 Max, darwin/arm64, 20 registries × 10 hook names, five 2-second repetitions, log output disabled and no filesystem/network I/O. Median direct resolution: 554824 → 737985 ns/op; 891870 → 1564506 B/op; 2371 → 2188 allocs/op. Median collection resolution: 225049 → 657873 ns/op; 185406 → 1463528 B/op; 616 → 1706 allocs/op. Both fixtures previously returned only 10 hooks and now retain 200; this measures the cost of retaining previously dropped work, not a speedup. Baseline was reconstructed from cb8d3bd in a detached worktree. The benchmark covers resolution only, not the entire sync lifecycle.
