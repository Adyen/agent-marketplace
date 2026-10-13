## Context

Every HTTP(S) clone and fetch runs Git with `GIT_CONFIG_GLOBAL=/dev/null` and `GIT_CONFIG_NOSYSTEM=1`, then supplies an allowlist of settings read from trusted system and global configuration through `GIT_CONFIG_KEY_n`. Transport classification canonicalized `redact.URL(source)`. Redaction rewrites `https://user:secret@host:8443/repo` as `***@host:8443/repo`, which the canonicalizer accepts as an SCP-style address.

## Decisions

1. Follow Git: no CLI-specific redirect refusal. Git's `initial` default sends credentials to the redirected URL. That requires the configured registry, which already holds the credential, to issue the redirect. The hosted Marketplace acquisition keeps `http.followRedirects=false`, because it handles service-owned tokens and publisher-supplied URLs.
2. Add `http.followRedirects` to the trusted allowlist. `gitAuthKey` already accepts URL-specific forms by their final component, and Git resolves precedence. A bare key is Git's boolean `true`, so the reader accepts it like `useHttpPath`.
3. Canonicalize `credentialFreeGitURL(source)` instead of the redacted URL in both trust classification and the transport diagnostic. Invalid HTTP URLs keep the existing fallback.

## Verification

Local `httptest` origins with synthetic credentials redirect to a second origin. Controls prove default redirects are followed for userinfo and host-scoped headers. Global and origin-scoped `followRedirects=false` refuse with no request at the target; another host's setting and a bare key leave redirects followed. The global/userinfo case and the userinfo-with-port diagnostic case failed before the change.
