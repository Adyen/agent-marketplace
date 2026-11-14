## Context

SourceForm is shared by Settings and first-run setup. Settings owns asynchronous mutation diagnostics, while the controller translates the server's unavailable-directory error. The GUI add-source admission boundary is the localapi path a real add-source request already follows: `validateAddSource` with a valid main ref and no configured allowlist, followed by the same UserPersonal `pipeline.EditLayerAtPath`/`EnsureRegistry` v4 editor used in configuration.go.

## Goals / Non-Goals

Implement the approved Add source form contract without changing server safety rules. Do not add a folder picker or check directory existence in the client.

## Decisions

- Keep validation in a reusable source-input module called by SourceForm. Mirror the concrete two-part admission authority — `localapi.validateAddSource` with a valid main ref and no configured allowlist, followed by the same UserPersonal `pipeline.EditLayerAtPath`/`EnsureRegistry` v4 editor used in configuration.go — rather than the broader CLI Git safety predicate or the browser URL parser. Dynamic organization policy and directory existence continue through the existing server checks; the client gains no policy or filesystem access.
- Use a shared table of inputs in Go and TypeScript tests, kept test-only, to compare client acceptance with the real admission functions rather than `giturl.ValidateURL` alone. No production Go validation or safety semantics change.
- Pass structured field errors into SourceForm and emit a field-edit event so Settings can clear the corresponding server error. Keep non-field diagnostics in Settings below the form and preserve existing asynchronous diagnostic ownership.
- Keep section headings unchanged. Apply one shared sans field-label class to the legend and input labels. Align the action to the right without a fixed width.
- Verify placement, computed label styles, accessible associations, and content-width action layout in a real browser at 1024px and the existing Settings minimum width. Retain light and dark before/after captures outside the repository.

## Risks / Trade-offs

Go and JavaScript string and URL handling differ. The parity table must include local paths, scp-style remotes, supported schemes, option-like inputs, traversal, whitespace, and unsafe transport helpers, exercised through the actual admission functions. Server-only checks (dynamic organization policy, directory existence) remain authoritative and must not become stricter client checks by accident.
