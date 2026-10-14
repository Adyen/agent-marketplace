## Why

Agent Marketplace runs Git for registry acquisition, so registry URLs should behave as they do with plain `git`. Git's default `http.followRedirects=initial` follows the first redirect and sends the configured credentials to the redirected URL. Only the configured registry can issue that redirect, and it already receives the credential, which is the same trust #432 accepts for credential helpers, unscoped extra headers and SSH (R3.2-global). Refusing credentialed redirects would break ordinary remotes, such as GitLab project URLs without `.git`.

The CLI did not fully follow Git, however. It ignored a user's own `http.followRedirects` setting, because only allowlisted credential and TLS settings are copied from trusted configuration. Separately, an HTTP(S) URL with userinfo and an explicit port was canonicalized after redaction as an SCP-style address. Such a URL then received no trusted settings at all, and its transport failures used the SSH diagnostic.

## What Changes

- Document that HTTP(S) acquisition follows Git's redirect semantics, including where credentials go.
- Copy `http.followRedirects` (including URL-specific forms and a bare key meaning `true`) from trusted system/global configuration, so a user's own Git redirect policy applies.
- Canonicalize the credential-free URL when classifying transports, so userinfo URLs with a port get trusted settings and the HTTP(S) diagnostic.

## Capabilities

### Modified Capabilities

- `git-authentication`: redirect policy follows Git; the copied allowlist includes `http.followRedirects`.

## Impact

Closes R3.2-redirect in #432 as an accepted contract: Git's redirect semantics. No acquisition that works today starts failing unless the user's trusted Git configuration refuses redirects. Default-branch lookup (`ls-remote`) still loads no trusted Git configuration, unchanged by this change.
