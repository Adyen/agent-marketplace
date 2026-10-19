## Why

#781. The case-only collision rule (#732) refuses every resource of one kind, target and scope whose names differ only in letter case, and it ignores `required`. Command and subagent names may contain upper-case letters (an OpenCode command is named after its file). So a command `Deploy` that the user selects from a registry or Marketplace blocked an organization-required command `deploy`: sync refused both with `bundle.conflict.duplicate-artifact-path`, exited with an error, and the required command was not installed. Reproduced at `ce1774b6` by a command test and an E2E table case.

Maintainer decision (2026-10-19): required means required, so the required resource is kept.

## What Changes

- In a case-only collision, a member selected by a required entry is installed when it outranks every other member: the others are not required, or it comes from a higher-precedence layer or file than each other required member. Each other member is omitted on that target with the `materialize.collision.shared-path` warning, which names both paths.
- Without such a member, every member is still refused, as before. That covers pairs where neither is required and pairs whose requirements tie.
- The rule holds on every host, like the case-only refusal it refines.

## Capabilities

### Modified Capabilities

- `harness-compilation`: case-only collisions keep the outranking required member.
- `resource-selection`: the shared-path requirement points to that rule for same-kind case variants.

## Impact

The folded pass of `resolver.MaterializationProjection` now covers every kind with its own native path on a target, not only kinds that share one. It displaces the other members when a required member outranks them, so the materializer's case-only refusal sees only the winner. Its warning names the required winner. `status` already omits displaced targets. Configurations without such a pair are unaffected; the pass only runs when a resource name contains an upper-case letter.
