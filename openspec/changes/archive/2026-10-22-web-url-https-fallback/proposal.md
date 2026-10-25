## Why

`install --from`, `list --from` and `remove --from` rewrite GitHub and GitLab web URLs to SSH. On a machine without an SSH key for the host, a public repository then fails with `git.auth-transport-failed`, although HTTPS would serve it. The only workaround, appending `.git` so the URL is not treated as a web URL, is not discoverable. #146 made the SSH failure actionable; this change avoids it when HTTPS works.

## What Changes

- A web URL's SSH form stays preferred. When it cannot be reached and the HTTPS form (`https://<host>/<project>.git`) can, `install --from` and `list --from` use the HTTPS form and print a warning naming both URLs. When the configuration already records either form for the same path and ref, install reuses it without probing. When neither can be reached, the SSH form and its diagnostic remain.
- The HTTPS form is checked against `allowed_registries` before it is probed or used.
- `remove --from <web URL>` matches a source stored in either form.
- Git URLs, `.git` URLs and directory sources are never probed.

## Capabilities

### New Capabilities

### Modified Capabilities
- `resource-installation`: web URL sources fall back to HTTPS.
- `resource-removal`: web URL removal matches the HTTPS form.

## Impact

`internal/pipeline` (`url_fallback.go`, `install.go`, `remove.go`) and `cmd/agent-marketplace/list.go`. A web URL `--from` runs one extra `git ls-remote` before acquisition. A configurable transport preference is a possible follow-up.
