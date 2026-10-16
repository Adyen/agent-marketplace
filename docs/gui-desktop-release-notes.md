# GUI desktop release notes

## Organization-controlled release availability notice

The desktop GUI can compare its running version with the latest stable public GitHub release and show an availability notice. This does not download or install updates. The CLI does not perform release checks based on this GUI setting.

`gui.version_check` is an organization-only boolean in the v4 `gui` mapping. It defaults to `true`. Set it to `false` to skip the release request and notice. User-personal, repository-committed, and personal-local values are accepted for compatibility, ignored with a warning, and do not override the organization setting. The desktop GUI reads and writes user-personal configuration; organization configuration is read-only to the GUI.

> **Reader-first rollout — do not deploy this key until every managed client runs a release whose CLI reads the `gui` mapping. Older v4 clients reject the unknown top-level `gui` field, and strict legacy readers reject `gui` too. Until the fleet upgrade is verified, leave the key absent.**

See [GUI release checks in the configuration reference](../site/docs/reference/configuration.md#gui-release-checks).
