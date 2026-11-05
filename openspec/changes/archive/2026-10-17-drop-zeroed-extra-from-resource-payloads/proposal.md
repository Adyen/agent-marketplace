## Why

Every skill, command, subagent and hook installed from a Marketplace payload compiles with a spurious `Extra: null` frontmatter field (#724). The canonical encoder marshals a copy of the resource with `Extra` zeroed, and these IR types serialize that field as `"Extra": null`. The decoder then keeps the member as an extension field.

The Marketplace API requirement "Canonical hook metadata normalization is resource-scoped" deliberately keeps decoding unchanged for payloads already published, so installed bytes don't churn and mixed-version teams don't drift. The fix therefore belongs in the encoder (maintainer decision on #724).

## What Changes

- The canonical encoder omits the zeroed `Extra` member, and an extension field named exactly `Extra` with a null value. Resources published from now on install without the line on every client version.
- Decoding is unchanged.

## Capabilities

### Modified Capabilities

- `harness-compilation`: newly encoded resource payloads carry no zeroed `Extra` member.

## Impact

`pkg/harness-compilation/artifact` encoder only.
- Payloads built after this change drop the member and get new content digests when the registry revision is next published. Payloads already published keep their bytes, digests and output.
- The index-only hook payload comparison re-encodes both sides with the same encoder, so it stays equal for earlier payloads.
- The source-revision fallback digest uses its own serialization and is unaffected.
