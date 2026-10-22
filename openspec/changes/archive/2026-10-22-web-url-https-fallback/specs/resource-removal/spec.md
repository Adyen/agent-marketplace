## ADDED Requirements

### Requirement: Web URL removal matches the HTTPS fallback

`remove --from` with a GitHub or GitLab web URL SHALL target a configured source recorded in the web URL's SSH form or in its anonymous HTTPS form `https://<host>/<project>.git`, so the URL used to install a source removes it. Only entries matching the requested `--path` and `--ref` SHALL count, and entries in the target scope, including entries from files it includes, SHALL decide before entries in other layers. When both forms match at the same level, the SSH form MUST be targeted.

#### Scenario: Web URL removes a source stored in HTTPS form

- **WHEN** only the HTTPS form of a web URL is configured and the user runs `remove --from` with that web URL
- **THEN** removal MUST target the HTTPS source

#### Scenario: Web URL prefers a source stored in SSH form

- **WHEN** the SSH form of a web URL is configured, alone or together with its HTTPS form
- **THEN** removal MUST target the SSH source

#### Scenario: Web URL removal follows the entry matching the request

- **WHEN** the target scope, directly or through an included file, or the requested `--path` or `--ref` matches only the HTTPS form, while an SSH form exists in another scope or with other qualifiers
- **THEN** removal MUST target the matching HTTPS source
