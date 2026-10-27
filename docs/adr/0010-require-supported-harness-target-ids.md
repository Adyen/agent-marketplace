# ADR 0010: Require supported harness target IDs

**Status:** Accepted (2026-10-26)

## Decision

**Your CLI must understand every tool named in the configuration.**

In the configuration contract, a tool is a harness target such as Factory
Droid (`factory`) or Claude Code (`claude`). "Understand" means that the
running CLI supports that target ID; it does not require the tool's application
to be installed.

Keep strict validation. An unknown harness target ID is a configuration error,
not a warning and not an entry to silently ignore. A mixture of supported and
unknown IDs does not authorize using just the supported subset. This applies
to target IDs in harness selections and per-target materialization settings.
Configuration-dependent operations must refuse the invalid configuration;
they must not silently remove, rename, or rewrite the unknown entry.

This selects option 1 from issue #935. It preserves existing runtime behavior,
corrects the unreachable TUI preservation promises, and aligns tests with the
real configuration-loading path. The normative contracts are the
[consumer configuration format specification](../../openspec/specs/consumer-config-format/spec.md)
and [TUI interactions specification](../../openspec/specs/tui-interactions/spec.md).
This ADR records the rationale; it does not supersede specifications.

## Examples

These are harness-list excerpts from otherwise valid v4 configuration:

```yaml
harnesses: [factory, claude]
```

Both IDs are supported: ordinary loading and saving continue unchanged.

```yaml
harnesses: [factory, future-tool]
```

If this CLI does not support `future-tool`, it refuses the configuration rather
than continuing with Factory alone. The user must upgrade to a CLI that supports
the target or explicitly correct/remove the entry. The refusal does not edit
the configuration for them.

```yaml
harnesses: [factory, cluade]
```

The misspelling is likewise an error, not a warning followed by a successful
Factory-only operation.

## Context

The TUI specification promised to preserve unknown selected IDs on save and
resize. Configuration validation rejects such IDs before normal TUI loading.
Model tests injected unknown IDs directly into form state and therefore did
not establish that users could reach that behavior through the binary.

## Alternative rejected

Warning-only forward compatibility would let an older CLI use supported
targets while retaining IDs introduced by a newer client. It could help teams
share configuration across CLI versions, but would also let misspellings become
partially effective configuration. Correct handling would require coordinated
loading, saving, materialization, and no-supported-target behavior.

We are not adding that product behavior merely to complete terminal coverage.
Teams must use a CLI version that understands every configured target.

## Consequences and verification

- Keep runtime validation, diagnostic identities and invalid-input write guards.
- Replace unknown-ID save/resize promises with supported-ID outcomes.
- Use supported selections in model tests; do not claim public compatibility
  from injected states the configuration loader rejects.
- Prove visible refusal and unchanged configuration, acquisition locks and
  installed resources through real-terminal journeys, with supported-input
  recovery as a positive control.
- Keep unsupported consumer-format refusal and pre-existing dependency-journal
  recovery contracts unchanged. This does not impose a blanket no-write rule
  on logging or recovery of already-admitted transactions.

## References

- [Decision issue #935](https://github.com/Adyen/agent-marketplace/issues/935)
- [Terminal coverage epic #928](https://github.com/Adyen/agent-marketplace/issues/928)
