## 1. Server definition interpolation and startup

- [ ] 1.1 Add red tests for `${VAR}`/`${env:VAR}` expansion in args, env values, url, and headers; child environment limited to declared keys; unset-variable and `${file:...}` failures naming field and variable; and HTTP initialization rejection mapped to `mcp.start-failed` with expanded values redacted. Annotate `// spec: code-mode-mcp/environment-references-expanded-before-start`, `.../unresolvable-reference-fails-before-start`, and `.../remote-initialization-rejection-reports-start-failure`; verify the tests fail on the base.
- [ ] 1.2 Implement expansion after artifact verification and before pool keying and process start, extend redaction to expanded values, and classify handshake failures as `mcp.start-failed`; verify focused `internal/mcpclient` and `internal/codemode` tests pass.

## 2. Declared refs and script values

- [ ] 2.1 Add a red test for a repeated ref in `servers` binding once under its name and starting one process (`// spec: code-mode-mcp/repeated-declared-ref-binds-once`), then deduplicate before aliasing; verify it passes.
- [ ] 2.2 Add red tests for thrown errors being `Error` instances named `CodeModeError` with lowercase `code`/`message` and no Go-cased fields (`// spec: code-mode-mcp/failures-carry-stable-codes`), update the existing `err.Code` test, and implement; verify focused tests pass.
- [ ] 2.3 Add red tests for lowercase finding fields on catalog entries and `skills.load` results (`// spec: code-mode-mcp/catalog-and-skill-findings-use-documented-fields`), and implement JSON conversion for catalog and skill values while keeping per-call catalog mutation isolated; verify focused tests pass.
- [ ] 2.4 Add red tests for the bounded truncation marker with `bytes` and a prefix of at most 4 KiB cut on a UTF-8 boundary under the existing `// spec: code-mode-mcp/output-cap-truncates-to-valid-json` annotation, and implement; verify focused tests pass.

## 3. Catalog ref identity

- [ ] 3.1 Add red tests for ref prefix equal to `registry` including Marketplace entries, v4 directory and Git sources labeled by source name, stable suffixes for colliding labels across layers, and promotion of a ref recorded under an earlier label. Annotate `// spec: code-mode-mcp/ref-prefix-matches-registry-field`, `.../named-sources-label-refs-by-source-name`, `.../colliding-source-labels-disambiguated`, and `.../recorded-refs-remain-promotable`; verify the tests fail on the base.
- [ ] 3.2 Carry the v4 source name into the projected registry entry, label refs from it with the derived-label fallback, and set Marketplace `registry` to `marketplace:<slug>`; verify focused `internal/config`, `internal/codemode`, and `cmd/agent-marketplace` tests pass.

## 4. Tool surface and delivery

- [ ] 4.1 Update the `search` and `execute` tool descriptions to declare finding, truncation marker, and thrown error shapes, with a test (`// spec: code-mode-mcp/tool-descriptions-declare-script-visible-shapes`); reconcile `skills/skills/code-mode/SKILL.md` with the new ref, error, and truncation behavior.
- [ ] 4.2 Add a stdio E2E test that declares a remote server needing a header `${VAR}`, catches a coded error, and reads a truncated result; verify `CGO_ENABLED=0 go -C e2e test -run '^TestCodeMode' -count=1 .` passes.
- [ ] 4.3 Run `make spec-check` and `make validate-full`, and verify each succeeds.
- [ ] 4.4 Run `openspec archive fix-code-mode-contract-gaps --yes` in this pull request and verify the main `code-mode-mcp` spec contains the new and modified scenarios.
