# Coding Standards

These are review criteria for all code changes. They complement the architecture, testing, logging, and performance guidance in [AGENTS.md](./AGENTS.md). Tool-enforced formatting and lint rules are intentionally not repeated here.

## Platform scope

- Support Unix-like systems only: macOS and Linux. Windows build or runtime compatibility is not a requirement.
- Do not add Windows-specific branches, build tags, path handling, abstractions, or tests unless the project's supported-platform decision changes.
- Remain portable across macOS and Linux. Use `filepath` for host filesystem paths and `path` for slash-separated artifact or `fs.FS` paths.

## Design

- Keep each policy in one authoritative place. Centralize target definitions, mappings, defaults, path rules, and diagnostic semantics instead of repeating switches or constants across packages.
- Extract genuinely shared behavior into focused helpers. Do not duplicate logic, and do not add extension points or abstractions for hypothetical future uses.
- Use names that expose domain intent. Prefer domain types over groups of loosely related primitives when the grouping carries invariants.
- Keep modules cohesive and boundaries explicit. Pure resolution and compilation logic must stay separate from filesystem, process, and network mutation.
- Preserve deterministic and idempotent behavior. Identical inputs must produce identical ordered output and bytes; sort map-derived output explicitly, and avoid rewriting files whose desired bytes are unchanged.
- Treat paths, roots, and symlinks as untrusted input. Validate containment and reject traversal or symlink escapes before filesystem mutation.
- Fail closed at destructive boundaries. Malformed, ambiguous, or unowned input must not trigger overwrite or prune behavior. Contain a failure to the narrowest safe unit so valid independent work can still proceed when the contract permits it.
- Never hide unsupported or lossy behavior. Return a stable coded diagnostic and omit the unsafe or unrepresentable output rather than silently dropping data or fabricating support.

## Tests

- Test observable contracts, not the implementation's restatement of itself. Expected values must be independently authored; do not derive them with the production helper, table, mapping, or algorithm under test.
- Do not write tautological or vacuous tests. Every test must be capable of failing for the regression it claims to cover. Negative, no-op, filtering, and redaction tests need a positive or non-vacuity control proving that the exercised path had something to reject, preserve, redact, or change.
- Start bug fixes with a regression test that fails against the buggy behavior, then make it pass with the fix.
- Assert the complete externally relevant outcome, including negative side effects. Filesystem failure tests must verify that user bytes remain unchanged and that no temporary or staging residue survives.
- Assert machine-readable contracts with stable diagnostic codes, typed errors, or `errors.Is`/`errors.As`; do not couple tests to incidental error prose.
- Use table-driven subtests for behavior matrices and byte-exact golden tests for generated layouts. Share fixtures and assertion helpers rather than repeating setup.
- Isolate tests from the developer's machine. Use temporary directories and scoped environment variables; default unit tests must not read or mutate the real home directory or depend on optional third-party executables or external network services.
- Follow the E2E test selection rules in [AGENTS.md](./AGENTS.md): prefer declarative cases for standard setup/run/assert workflows and standalone tests only when the framework cannot express the setup.

## Errors and observability

- Wrap errors with the failed operation and relevant context while preserving the cause with `%w`. Branch on error identity, not message text.
- Use structured `slog` fields for meaningful operation boundaries, decisions, counts, durations, and failures. Never log credentials, tokens, headers, or sensitive payloads.
- Treat stable diagnostic codes and severities as public behavior. Add or update tests whenever either changes.

## Performance

- Performance claims require measurements. A change to a user-visible hot path must retain a representative benchmark beside the affected package and report before/after time and allocation results.
- Keep setup out of benchmark timing, exercise realistic workloads, and report allocations. Do not trade correctness, determinism, or safety for an unmeasured optimization.
