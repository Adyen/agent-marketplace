## Context

Existing MCP JSON readers and canonical artifact decoding retain unknown fields in Extra. Six native MCP serializers emit those extras, while Codex omits unknown fields with a warning. Canonical resource identity and historical fallback projections depend on preserving the original resource.

## Goals / Non-Goals

**Goals:** Reserve publishing metadata only at native output, preserve intrinsic identity, and prove real consumer-path no-op behavior.

**Non-Goals:** Authoring validation, typed metadata, descriptions, README reads, UI or inspection changes, a new source-revision version, writer activation, ownership changes, and release automation.

## Decisions

Add one MCP-specific extra-key selector beside ExtraKeys. It excludes keys equal to `_agent-marketplace` under strings.EqualFold and delegates remaining keys to existing ExtraKeys behavior without changing the original map. Route the six JSON MCP serializers through that helper. Codex retains its typed serialization and excludes the same namespace from its unmodeled-extra warning predicate.

Filtering at ingestion or canonical decode would change intrinsic bytes and historical digests. A global ExtraKeys filter would affect unrelated resource kinds. Neither is needed. Namespace values are deliberately uninterpreted by this prerequisite; validation and display belong to the full publishing feature.

Use public compile/materialize tests across targets and scopes, verified canonical restoration, frozen source-revision vectors, and a real synchronization fixture with handwritten native content and byte/mtime controls. Existing source-revision writers remain unchanged. Archive this prerequisite independently of the full publishing change.

## Risks / Trade-offs

A pre-feature extension using this reserved namespace stops appearing in native output, but remains in canonical content. Other extras preserve their established behavior. A release containing this fence resolves native metadata emission only; any future committed format still needs released read support and executable compatibility evidence before writer activation.
