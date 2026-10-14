## Why

The upgrade notice from `add-upgrade-notice` can only be turned off with an environment variable or CI. Teams that manage their CLI versions through an organization configuration, and users who want a persistent choice, have no configuration-file way to opt out.

## What Changes

- The v4 root gains an optional `update_check` mapping with one field, `enabled`.
- `update_check` is accepted only in organization and user-personal configuration. The user value overrides the organization value. Repository and personal-local configuration reject it with `v4.layer-field`.
- `update_check: {enabled: false}` suppresses the release lookup and notice. The default remains enabled.
- The environment variable, `CI`, and command exclusions still suppress the check. `enabled: true` never overrides them.

## Capabilities

### Modified Capabilities

- `upgrade-notice`: adds the configuration opt-out to the suppression requirement.

## Impact

Adds the `update_check` v4 root field in `internal/config` and one lookup used by the CLI and TUI release-check seam. Legacy v2/v3 schemas are unchanged. Documentation for the configuration reference and the install guide mentions the new field.
