## Why

The shipped CLI already accepts only v4 consumer configuration, but some specs still describe legacy selection syntax or omit the format-validation boundary from acquisition-independent reads. Active plans also retain retired pre-v4 gate instructions. Align these descriptions with `consumer-config-format` without changing runtime behavior or unrelated versioned formats.

## What Changes

- Apply the exact-v4 root and accepted-fragment contract consistently to authored reads, settings, installation, and TUI startup.
- Describe Marketplace instance identity as acquisition evidence, not an authored legacy field, and name the existing `v4.legacy-key` refusal.
- Replace top-level collection and implicit required-catalog selection claims with source-bound v4 collection selectors and explicit `all: true`; prove that `required: true` alone selects nothing.
- Correct active harvest and native-hook plans without rewriting archived changes or removing lock, source-revision, operator, API, artifact, ownership, or native-hook compatibility.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `authored-configuration-reads`: Make format validation a prerequisite for acquisition-independent reads.
- `configuration-intent`: Require accepted v4 input and distinguish authored registration from acquired identity.
- `configuration-settings`: Preserve lock-independent settings reads only for validated consumer input.
- `config-layering`: Specify the exact diagnostic for legacy installation keys.
- `resource-installation`: Make existing-root validation precede batch installation.
- `resource-selection`: Specify source-bound collection selection and explicit required-group selection.
- `tui-startup`: Make v4 format refusal part of fail-closed startup.

## Impact

Specification and test changes only. Existing runtime-model unit tests remain useful internal controls but no longer stand in for authored v4 collection workflows. New consumer tests load actual v4 roots, and existing refusal, provenance, and acquisition-independent tests retain scenario bindings.
