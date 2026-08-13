## Context

Some resources are not useful unless another marketplace resource is selected.
For example, a knowledge skill may require an MCP server that can read its
authoritative documentation. Collections can select a known bundle, but they do
not express a functional relationship from one artifact to another or explain a
missing prerequisite after selection.

Prerequisites are authored by registry maintainers in resource metadata, while
selection and exclusion are explicit user or organization configuration. The
design must expose the relationship without allowing artifact metadata to
override configuration intent.

## Goals / Non-Goals

**Goals:**

- Let resources declare functional prerequisites through `metadata.requires`.
- Support structured declarations and a concise `kind:name` shorthand.
- Report unmet prerequisites during install, sync, status, and doctor.
- Provide an actionable install command that can include registry and path
  hints.
- Preserve all explicit selection and exclusion decisions.
- Keep requirement checks inexpensive when no selected resource declares one.

**Non-Goals:**

- Automatically select, install, or remove prerequisite resources.
- Resolve provider ambiguity across registries.
- Verify external authentication, authorization, or service availability.
- Treat functional prerequisites as organization-enforced `required: true`
  resources.

## Decisions

### Decision: Declare prerequisites in artifact metadata

Resources declare dependencies in `metadata.requires` because the relationship
belongs to the resource and should travel with it across registries and config
layers.

The supported structured fields are `kind`, `name`, `registry`, and `path`.
`kind` defaults to `skill`; `"mcp:genai-atlassian"` is accepted as shorthand
when provider hints are unnecessary.

Alternative considered:

- Define all dependencies in agent-marketplace config. Rejected because it
  duplicates artifact knowledge in every consuming config and allows the
  relationship to drift independently from the resource.

### Decision: Keep prerequisite handling warning-only

Requirement declarations are observational. Install materializes the resource
the user requested, then reports any unmet prerequisites found by the same
resolver used by sync, status, and doctor. No prerequisite declaration changes
configuration.

Rationale:

- Registry-authored metadata must not outrank explicit user configuration.
- Warning-only behavior has no provider-resolution or transitive-resolution
  conflicts.
- Networked resources such as MCP servers may require authentication and should
  remain deliberate user choices.

Alternative considered:

- Automatically install uniquely resolvable prerequisites. Rejected because a
  transitive install could silently clear an explicit exclude, making artifact
  metadata override user intent. Provider ambiguity and transitive traversal
  also added complexity disproportionate to the convenience of avoiding one
  explicit install command.

### Decision: Explicit excludes remain visible as unmet requirements

An explicit exclude is never removed because another resource requires it. For
non-required resources, the exclude wins and the CLI still warns while the
requiring resource remains selected, because the runtime capability is
incomplete and the warning explains why. Resources protected by registry
entries marked `required: true` remain immune to excludes, stay selected, and
satisfy the prerequisite without a warning.

Alternative considered:

- Suppress warnings for explicitly excluded prerequisites. Rejected because it
  would hide a known broken runtime state.

### Decision: Satisfy requirements by kind and name

A selected resource satisfies a prerequisite when both kind and name match.
`registry` and `path` are remediation hints used to construct the suggested
install command; they do not constrain satisfaction.

Rationale:

- Selection already prevents unresolved same-kind/name collisions.
- Provider hints help users install the intended resource without making the
  runtime relationship depend on registry layout.

### Decision: Parse metadata lazily from the resource IR

The catalog reads requirements from `ir.Resource.Extra` only when validating a
selected resource. Catalog entries do not retain another requirements slice.

Rationale:

- Avoids allocations on catalog and resolver hot paths when resources have no
  requirements.
- Keeps the original resource metadata as the source of truth.

## Risks / Trade-offs

- **Risk: warning fatigue for deliberately excluded prerequisites.** The warning
  is grouped with other diagnostics and includes a direct remediation command.
- **Risk: a same-kind/name resource from an unintended registry satisfies the
  declaration.** Normal collision handling still rejects ambiguous selected
  resources; provider hints remain guidance rather than policy.
- **Risk: users assume installation verifies MCP readiness.** Documentation
  states that external authentication and service health remain outside the
  marketplace's scope.
- **Trade-off: prerequisite installation requires a second command.** This is
  intentional to preserve an explicit configuration mutation boundary.

## Open Questions

- None. Prerequisite metadata is advisory, explicit configuration always wins,
  and unmet prerequisites remain visible.
