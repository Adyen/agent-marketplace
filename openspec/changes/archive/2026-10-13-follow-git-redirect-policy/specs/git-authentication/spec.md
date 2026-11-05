## MODIFIED Requirements

### Requirement: Include support does not broaden authentication authority

Only the existing credential and TLS allowlist and the `http.followRedirects` redirect policy SHALL reach HTTP(S) acquisition subprocesses. Unrelated execution settings and TLS verification disabling MUST NOT become active through an include. Conditional `includeIf` entries MUST remain unsupported and return `git.auth-config-untrusted`. Repository-local credential, HTTP, include, askpass, and worktree-configuration settings MUST remain refused independently of ordinary system/global include support.

#### Scenario: Conditional includes remain explicitly refused

- **WHEN** a system or global chain contains a conditional include
- **THEN** acquisition MUST refuse it with `git.auth-config-untrusted`, including conditions that do not match

#### Scenario: Included settings do not activate unrelated Git behavior

- **WHEN** an ordinary trusted include also declares hooks, URL rewrites, or TLS verification disabling
- **THEN** only allowlisted authentication settings MUST be applied

#### Scenario: Worktree authentication does not gain authority

- **WHEN** acquisition runs from a caller worktree with repository-local authentication settings
- **THEN** those caller settings MUST NOT supply acquisition authentication
- **AND** authentication settings in the registry cache's repository-local or worktree configuration MUST remain refused

## ADDED Requirements

### Requirement: Acquisition follows the Git redirect policy

HTTP(S) registry clone and fetch SHALL follow redirects exactly as Git does. Without a trusted redirect setting, Git's default `http.followRedirects=initial` applies: Git follows a redirect of the initial request and uses the redirected URL, with the configured credentials, for follow-up requests. Agent Marketplace MUST NOT add its own redirect refusal. An `http.followRedirects` value from trusted system or global Git configuration, including URL-specific forms and a bare key meaning `true`, MUST apply to acquisition, including for registry URLs that carry userinfo and an explicit port. A redirect refused by that setting MUST fail with the withheld transport diagnostic and MUST NOT expose credentials.

#### Scenario: Acquisition follows the default Git redirect behavior

- **WHEN** an HTTP(S) registry redirects clone or fetch, the request carries URL userinfo or a host-scoped extra header, and no trusted configuration sets `http.followRedirects`
- **THEN** exact and mutable acquisition MUST follow the redirect and succeed, as Git does

#### Scenario: Trusted redirect policy applies to acquisition

- **WHEN** trusted global configuration sets `http.followRedirects=false` globally or for the registry URL, and the registry redirects clone or fetch
- **THEN** exact and mutable acquisition MUST fail with `git.auth-transport-failed`, the redirect target MUST receive no request, and the diagnostic MUST NOT contain the credential
- **AND** the same setting scoped to another host, or a bare `followRedirects` key, MUST leave the redirect followed

#### Scenario: Userinfo URLs with a port use the HTTPS diagnostic

- **WHEN** an HTTPS registry URL carries userinfo and an explicit port and its transport fails
- **THEN** the failure MUST use the withheld HTTP(S) transport diagnostic rather than the SSH diagnostic
