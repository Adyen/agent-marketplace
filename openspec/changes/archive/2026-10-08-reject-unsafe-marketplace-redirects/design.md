## Context

Nginx matches compatibility routes against its decoded URI. Copying a
backslash from that path into a redirect changes how browsers interpret its
authority. Tabs and line breaks can also expose a second leading slash
when the browser removes them, or corrupt the response headers. Existing
configuration-string tests and the mock browser proxy do not execute this
boundary.

## Goals / Non-Goals

- Reject unsafe compatibility paths without emitting a `Location` header.
- Keep ordinary route and query migration behavior unchanged.
- Cover every supplied proxy with the actual pinned Nginx runtime.
- Reject unsupported encoded browser bases before starting the service.
- Do not relax OIDC or frontend administration destination validation.

## Decisions

Reject decoded backslashes and ASCII control characters immediately before
emitting a legacy compatibility redirect. Returning HTTP 400 from a Nginx
location is a terminal action. Keep the destination in a named capture:
evaluating the safety regex clears Nginx's positional captures, even on a
safe request. Match line breaks in the compatibility location so a nested
decoded newline cannot bypass rejection by falling through to the web
interface.

Use isolated, read-only Nginx containers with no published ports or network
access. Test requests execute on container loopback. A dedicated Make target
requires the test witness to run and pass; ordinary root tests can run
without Docker. The CI deployment check invokes the mandatory target for
deployment and regression-test changes.

Reject browser-base paths requiring percent encoding. Supporting the encoded
prefix only in the login validators is insufficient: the supplied Nuxt runtime
returns 404 for an encoded application base because its mount and decoded
request path disagree. Fail during configuration loading with an error naming
`instance.browser_base`, without reflecting the URL. Keep canonical roots and
ordinary subpaths supported, and retain all existing destination safeguards.

## Risks / Trade-offs

Malformed legacy browser paths become explicit client errors. Encoded controls
and backslashes in query strings remain valid because only the decoded path
is checked.
The deployment gate requires Docker and the configured pinned Nginx image.
Operators using an encoded browser base must choose an unescaped path and align
it with the proxy and Nuxt base rather than deploy an unusable route.
