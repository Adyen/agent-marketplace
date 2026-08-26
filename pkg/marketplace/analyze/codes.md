# Marketplace analyzer finding codes

This catalog mirrors the stable code vocabulary in `codes.go`. Every code must
have an entry below; `TestFindingCodes_Documented` and
`TestPolicyCodes_Documented` enforce parity.

## Analyzer detections

| Code | Severity | Description |
|---|---|---|
| `marketplace.find.html-comment` | warn | HTML-style comments that may hide instructions from renderers. |
| `marketplace.find.zero-width` | warn | Zero-width or invisible Unicode characters outside accepted presentation sequences. |
| `marketplace.find.terminal-control` | reject | ANSI / terminal escape sequences. |
| `marketplace.find.unsafe-bidi` | reject | Bidirectional override characters that could mislead review. |
| `marketplace.find.dangerous-url` | warn | URL with dangerous scheme or target. |
| `marketplace.find.remote-reference` | warn | Reference to remote resource or payload. |
| `marketplace.find.executable-asset` | reject | Opaque native, packaged, or binary executable asset. |
| `marketplace.find.executable-script` | warn | Readable script asset or shebang script. |
| `marketplace.find.asset-path-escape` | reject | Absolute or traversing resource asset path. |
| `marketplace.find.dynamic-execution` | warn | Raw dynamic-code execution API reference. |
| `marketplace.find.tool-grant` | warn | Declared tool grant or permission. |
| `marketplace.find.hook-capability` | warn | Declared hook capability. |
| `marketplace.find.mcp-capability` | warn | Declared MCP server capability. |
| `marketplace.find.suspicious-path` | warn | Reference to a narrowly identified sensitive filesystem target. |
| `marketplace.find.obfuscated-instruction` | reject | Encoded or confusable hostile instruction. |

## Policy decisions

These codes record an operator policy decision taken during ingest, not an
analyzer detection. A reject-severity policy finding strips the installable
artifact; a warn-severity one leaves the resource installable.

| Code | Severity | Description |
|---|---|---|
| `marketplace.policy.license-not-allowed` | reject | The resource declares a license that `policy.license_allowlist` does not contain. |
| `marketplace.policy.license-undeclared` | reject or warn | The resource declares no license at all, and an allowlist is configured. The severity follows `policy.license_undeclared` (`reject` by default, `warn` when opted in); `allow` emits no finding. |
| `marketplace.policy.artifact-too-large` | reject | The built artifact exceeds `policy.limits.artifact_bytes`. |
