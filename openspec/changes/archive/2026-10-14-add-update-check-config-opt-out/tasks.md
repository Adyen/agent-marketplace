## Implementation

- [x] Accept `update_check: {enabled}` as a v4 organization and user-personal root field, merged field by field within a layer, with the user value overriding the organization value.
- [x] Reject `update_check` in repository and personal-local configuration with `v4.layer-field`.
- [x] Gate the CLI and TUI release check on the effective setting, after the environment, `CI`, and command suppression rules.
- [x] Document the field in the configuration reference, the layer reference, the install guide, and the README.
- [x] Verify scoped tests, `make spec-check`, lint, build, and `make validate`.
