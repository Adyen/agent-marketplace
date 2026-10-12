## ADDED Requirements

### Requirement: Source-anchored local resource inspection

The TUI SHALL browse local disk resources from their ingested source bundle, whether the configured registry path is absolute or relative. Primary previews, supporting-file browsing, and captured preview content MUST remain independent of the consumer working directory and MUST NOT substitute same-named consumer files. Inspection MUST retain existing read bounds and filesystem safety policy, fail visibly for missing or unsafe content, and leave source files, configuration, acquisition locks, and materialized resources unchanged. Marketplace inspection and authoritative exact Git snapshot content MUST retain their existing source identity.

#### Scenario: Local disk resources remain browsable outside their registry

- **WHEN** a user opens a skill or file-backed resource from an absolute or relative local registry path while the consumer working directory is elsewhere
- **THEN** the TUI MUST show the correct source tree and primary preview, allow supporting-file previews for skills, and capture content from that source

#### Scenario: Consumer files cannot substitute local source content

- **WHEN** the consumer working directory contains different files at the same bundle-relative paths as a local resource
- **THEN** the TUI MUST browse and capture the registry content rather than the consumer files, without changing either source or consumer state

#### Scenario: Missing or unsafe local content fails read-only

- **WHEN** a local resource's primary content disappears or its preview payload becomes unsafe under the existing filesystem policy after discovery
- **THEN** inspection MUST fail visibly without displaying substituted or unsafe payload content or changing source files, configuration, acquisition locks, or materialized resources

#### Scenario: Authoritative remote inspection paths remain unchanged

- **WHEN** a user inspects verified Marketplace content or an exact Git snapshot whose display path differs from its authoritative source path
- **THEN** inspection MUST continue to use that verified or exact content rather than consumer files or a logical display-cache path
