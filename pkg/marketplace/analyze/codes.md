# Marketplace analyzer finding codes

This catalog mirrors the stable code vocabulary in `codes.go`. Every code must
have an entry below; `TestFindingCodes_Documented` enforces parity.

| Code | Severity | Description |
|---|---|---|
| `marketplace.find.html-comment` | reject | HTML-style comments that may hide instructions from renderers. |
| `marketplace.find.zero-width` | reject | Zero-width or invisible Unicode characters. |
| `marketplace.find.terminal-control` | reject | ANSI / terminal escape sequences. |
| `marketplace.find.unsafe-bidi` | reject | Bidirectional override characters that could mislead review. |
| `marketplace.find.dangerous-url` | reject | URL with dangerous scheme or target. |
| `marketplace.find.remote-reference` | warn | Reference to remote resource or payload. |
| `marketplace.find.executable-asset` | reject | Asset that appears executable or packaged. |
| `marketplace.find.tool-grant` | warn | Declared tool grant or permission. |
| `marketplace.find.hook-capability` | warn | Declared hook capability. |
| `marketplace.find.mcp-capability` | warn | Declared MCP server capability. |
| `marketplace.find.suspicious-path` | reject | Path traversal or suspicious file path. |
| `marketplace.find.obfuscated-instruction` | reject | Encoded or obfuscated instruction. |
