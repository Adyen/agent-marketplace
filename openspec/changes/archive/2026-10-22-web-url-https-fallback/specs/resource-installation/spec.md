## ADDED Requirements

### Requirement: Web URL sources fall back to HTTPS

`install --from` and `list --from` SHALL keep the SSH form of a GitHub or GitLab web URL as the preferred transport. When `git ls-remote` cannot reach that SSH form and can reach the HTTPS form `https://<host>/<project>.git`, the command SHALL use the HTTPS form for acquisition, SHALL report the switch in a warning that names both URLs, and, for install, SHALL record the HTTPS form as the source URL. The probe uses no Git credential helper, so a repository that requires authentication falls back only when HTTPS can authenticate otherwise, for example through `GIT_ASKPASS` or `~/.netrc`. When the merged configuration already records the SSH or HTTPS form for the same path and ref, `install --from` and `list --from` SHALL reuse that form without probing, install preferring the target scope's entries, so install never adds a second source for the same repository. The HTTPS form MUST satisfy `allowed_registries` before it is probed or used; a refused HTTPS form MUST leave the SSH form in use without probing either form. When neither form can be reached, the command MUST keep the SSH form and its existing diagnostic. Git URLs, URLs ending in `.git`, and directory sources MUST NOT be probed.

#### Scenario: Unreachable SSH web URL uses HTTPS

- **WHEN** `install --from` or `list --from` receives a web URL whose SSH form cannot be reached and whose HTTPS form can
- **THEN** the command MUST acquire the registry from the HTTPS form and print a warning naming both URLs
- **AND** a source written by install MUST record the HTTPS form

#### Scenario: Configured form of a web URL is reused

- **WHEN** `install --from` or `list --from` receives a web URL whose SSH or HTTPS form the merged configuration already records for the same path and ref
- **THEN** the command MUST reuse the recorded form without probing either form
- **AND** install MUST NOT add a second source for the repository

#### Scenario: Reachable SSH web URL keeps SSH

- **WHEN** the SSH form of a web URL can be reached
- **THEN** the command MUST use the SSH form and MUST NOT probe the HTTPS form

#### Scenario: Unreachable web URL keeps the SSH diagnostic

- **WHEN** neither the SSH nor the HTTPS form of a web URL can be reached
- **THEN** the command MUST keep the SSH form so acquisition reports the existing SSH diagnostic

#### Scenario: Blocked HTTPS fallback is not probed

- **WHEN** `allowed_registries` refuses the HTTPS form of a web URL
- **THEN** the command MUST keep the SSH form without probing either form

#### Scenario: Fallback probe ignores credential helpers

- **WHEN** a Git credential helper, including one from the current directory's repository configuration, could authenticate the HTTPS form
- **THEN** the reachability probe MUST NOT use it

#### Scenario: Non-web sources are not probed

- **WHEN** `--from` names a Git URL, a URL ending in `.git`, or a directory
- **THEN** the command MUST use it unchanged without a reachability probe
