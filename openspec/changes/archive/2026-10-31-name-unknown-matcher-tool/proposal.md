## Why

When a cross-harness hook matcher names a tool that the source harness does not declare, the refusal says only that the matcher "cannot be decoded" (#962). A Factory hook matching `Edit|Create|MultiEdit` is refused for Devin because `MultiEdit` is a Claude alias, not a Factory name, but the diagnostic points the author at the wrong place and names neither the tool nor the vocabulary it was read in.

## What Changes

- When decoding fails because a matcher token is not a tool name of the source harness, and another shipped harness catalog declares the name, the `hook.matcher-untranslatable` diagnostic names that one tool and the harness that declares it. A name no catalog declares keeps the generic message.
- Every other decode failure and every coverage failure keeps its current static message and hint, and the matcher is still not quoted in those.

## Release note

A hook matcher refused during cross-harness translation now names the tool when it belongs to another harness's vocabulary, for example `MultiEdit` is a Claude tool name, not a Factory one.

## Capabilities

### Modified Capabilities

- `hook-sync-diagnostics`: the decode refusal may quote a single tool name taken from a shipped catalog.

## Impact

Internal to `pkg/harness-compilation/toolpolicy` and `pkg/harness-compilation/hookpolicy`. The diagnostic code, severity, suppression and translation behavior are unchanged. The quoted token comes from a shipped catalog, not from the user's payload, so the value-free rule holds for arbitrary input.
