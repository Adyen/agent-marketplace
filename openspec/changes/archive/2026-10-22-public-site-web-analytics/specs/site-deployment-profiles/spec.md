## ADDED Requirements

### Requirement: Public website analytics is limited to the published Pages site

The publishing build of the public project website SHALL load the Cloudflare Web Analytics beacon on every page when a Cloudflare Web Analytics token is configured for that build. The beacon SHALL NOT set cookies or require consent, and its token SHALL be the only build-specific value it carries. The documentation-only artifact, and any public-site build without a configured token, SHALL NOT load the beacon or any other third-party analytics script. A configured token that is not a 32-character lowercase hexadecimal value SHALL fail the build.

#### Scenario: Published public site loads Cloudflare Web Analytics

- **WHEN** the public project website is built with a valid Cloudflare Web Analytics token
- **THEN** every generated page MUST load the Cloudflare Web Analytics beacon with that token
- **AND** an invalid configured token MUST fail the build

#### Scenario: Unpublished and self-hosted builds load no analytics

- **WHEN** the documentation-only artifact is built, with or without a configured token, or the public project website is built without a token
- **THEN** no generated page MUST load the Cloudflare Web Analytics beacon
