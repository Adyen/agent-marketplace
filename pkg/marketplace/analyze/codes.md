# Marketplace analyzer finding codes

This catalog mirrors the stable code vocabulary in `codes.go`. Every code must
have an entry below; `TestFindingCodes_Documented` and
`TestPolicyCodes_Documented` enforce parity.

## Analyzer detections

| Code | Severity | Owning analyzer | Description |
|---|---|---|---|
| `marketplace.find.html-comment` | warn | `marketplace.hidden-content` | HTML-style comments that may hide instructions from renderers. |
| `marketplace.find.zero-width` | warn | `marketplace.hidden-content` | Zero-width or invisible Unicode characters outside accepted presentation sequences. |
| `marketplace.find.terminal-control` | reject | `marketplace.hidden-content` | ANSI / terminal escape sequences. |
| `marketplace.find.unsafe-bidi` | reject | `marketplace.hidden-content` | Bidirectional override characters that could mislead review. |
| `marketplace.find.obfuscated-instruction` | reject | `marketplace.obfuscation` | Encoded or confusable hostile instruction. |
| `marketplace.find.dynamic-execution` | warn | `marketplace.obfuscation` | Raw dynamic-code execution API reference. |
| `marketplace.find.executable-asset` | reject | `marketplace.assets` | Opaque native, packaged, or binary executable asset. |
| `marketplace.find.executable-script` | warn | `marketplace.assets` | Readable script asset or shebang script. |
| `marketplace.find.asset-path-escape` | reject | `marketplace.assets` | Absolute or traversing resource asset path. |
| `marketplace.find.dangerous-url` | warn | `marketplace.references` | URL with dangerous scheme or target. |
| `marketplace.find.remote-reference` | warn | `marketplace.references` | Reference to remote resource or payload. |
| `marketplace.find.suspicious-path` | warn | `marketplace.references` | Reference to a narrowly identified sensitive filesystem target. |
| `marketplace.find.tool-grant` | warn | `marketplace.capabilities` | Declared tool grant or permission. |
| `marketplace.find.hook-capability` | warn | `marketplace.capabilities` | Declared hook capability. |
| `marketplace.find.mcp-capability` | warn | `marketplace.capabilities` | Declared MCP server capability. |

## Policy decisions

These codes record an operator policy decision taken during ingest, not an
analyzer detection. A reject-severity policy finding strips the installable
artifact; a warn-severity one leaves the resource installable.

| Code | Severity | Description |
|---|---|---|
| `marketplace.policy.license-not-allowed` | reject | The resource declares a license that `policy.license_allowlist` does not contain. |
| `marketplace.policy.license-undeclared` | reject or warn | The resource declares no license at all, and an allowlist is configured. The severity follows `policy.license_undeclared` (`reject` by default, `warn` when opted in); `allow` emits no finding. |
| `marketplace.policy.artifact-too-large` | reject | The built artifact exceeds `policy.limits.artifact_bytes`. |
| `marketplace.policy.audit-unavailable` | warn | A gating analyzer could not complete; the resource is visible but not installable until the analyzer succeeds. This warning is non-consentable. |
