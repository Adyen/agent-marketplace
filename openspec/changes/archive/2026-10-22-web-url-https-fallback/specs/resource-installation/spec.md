## ADDED Requirements

### Requirement: Web URL sources fall back to anonymous HTTPS

`install --from` and `list --from` SHALL keep the SSH form of a GitHub or GitLab web URL as the preferred transport. When `git ls-remote` cannot reach that SSH form and can reach the anonymous HTTPS form `https://<host>/<project>.git`, the command SHALL use the HTTPS form for acquisition and, for install, record it as the source URL. The HTTPS form MUST satisfy `allowed_registries` before it is probed or used; a refused HTTPS form MUST leave the SSH form in use without probing either form. When neither form can be reached, the command MUST keep the SSH form and its existing diagnostic. Git URLs, URLs ending in `.git`, and directory sources MUST NOT be probed.

#### Scenario: Unreachable SSH web URL uses anonymous HTTPS

- **WHEN** `install --from` or `list --from` receives a web URL whose SSH form cannot be reached and whose HTTPS form can
- **THEN** the command MUST acquire the registry from the HTTPS form
- **AND** a source written by install MUST record the HTTPS form

#### Scenario: Reachable SSH web URL keeps SSH

- **WHEN** the SSH form of a web URL can be reached
- **THEN** the command MUST use the SSH form and MUST NOT probe the HTTPS form

#### Scenario: Unreachable web URL keeps the SSH diagnostic

- **WHEN** neither the SSH nor the HTTPS form of a web URL can be reached
- **THEN** the command MUST keep the SSH form so acquisition reports the existing SSH diagnostic

#### Scenario: Blocked HTTPS fallback is not probed

- **WHEN** `allowed_registries` refuses the HTTPS form of a web URL
- **THEN** the command MUST keep the SSH form without probing either form

#### Scenario: Non-web sources are not probed

- **WHEN** `--from` names a Git URL, a URL ending in `.git`, or a directory
- **THEN** the command MUST use it unchanged without a reachability probe
