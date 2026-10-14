## ADDED Requirements

### Requirement: Marketplace browser bases are canonical

A configured Marketplace browser base SHALL be an absolute public URL whose path is canonical and safe to use as a same-origin browser-route prefix. Marketplace operator configuration SHALL reject repeated leading separators, backslashes, control characters, and literal or encoded dot segments instead of silently selecting a different browser route.

#### Scenario: Unsafe browser bases fail during configuration loading

- **WHEN** a Marketplace operator configures a browser base with a non-canonical or unsafe path
- **THEN** configuration loading MUST fail with an error identifying `instance.browser_base` without exposing URL credentials or query values
- **AND** canonical root and subpath browser bases MUST remain valid

### Requirement: Browser authentication routes remain aligned with the deployment base

A root-mounted Marketplace deployment that enables browser administration SHALL configure the Marketplace browser base at that root. Supplied deployment defaults and operator guidance SHALL keep the web-interface base, administration destinations, sign-in error routes, and Marketplace return route aligned while preserving the absent-base legacy fallback for configurations that do not select the root-mounted profile.

#### Scenario: Root-mounted administration uses root browser routes

- **WHEN** a Marketplace is deployed with its web interface and configured browser base at the origin root
- **THEN** login and callback destinations MUST accept `/admin` and descendants and reject legacy `/marketplace/admin` destinations
- **AND** browser failures and missing destinations MUST use `/auth/error` and `/admin` respectively

#### Scenario: Subpath and absent browser bases retain their route contracts

- **WHEN** administration uses an explicit subpath browser base or no browser base
- **THEN** login, callback, error, return, and fallback routes MUST use the configured subpath or the documented legacy `/marketplace` paths respectively
