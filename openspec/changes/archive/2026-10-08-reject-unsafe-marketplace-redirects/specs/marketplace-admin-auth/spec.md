## MODIFIED Requirements

### Requirement: Marketplace browser bases are canonical

A configured Marketplace browser base SHALL be an absolute public URL whose path is canonical and safe to use as a same-origin browser-route prefix. Marketplace operator configuration SHALL reject repeated leading separators, backslashes, control characters, literal or encoded dot segments, and paths containing or requiring percent encoding instead of silently selecting a different or unsupported browser route.

#### Scenario: Unsafe browser bases fail during configuration loading

- **WHEN** a Marketplace operator configures a browser base with a non-canonical or unsafe path
- **THEN** configuration loading MUST fail with an error identifying `instance.browser_base` without exposing URL credentials or query values
- **AND** canonical root and unescaped subpath browser bases MUST remain valid

#### Scenario: Unsupported encoded browser bases fail during configuration loading

- **WHEN** a Marketplace operator configures a browser-base path containing percent escapes, spaces, or non-ASCII characters requiring percent encoding
- **THEN** configuration loading MUST reject it with an error identifying `instance.browser_base` before exposing inconsistent browser or administration routes
- **AND** supported root and unescaped subpath bases MUST remain accepted
