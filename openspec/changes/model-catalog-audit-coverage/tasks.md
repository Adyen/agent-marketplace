## 1. Red tests

- [ ] 1.1 Add the Gemini API docs fixture under `tools/model-catalog-audit/testdata/` and a failing parser test (AC1)
- [ ] 1.2 Add failing audit tests for `catalog.model-missing` on `gemini-4-argon` with and without the catalog entry (AC2)
- [ ] 1.3 Add failing tests for `source.inventory-unparsed` naming the source (AC3, generic inventory path)
- [ ] 1.4 Add failing tests for `catalog.family-pin-stale` on the Factory Haiku pin, and no finding when current (AC4)
- [ ] 1.5 Add the Devin CLI snapshot under `tools/model-catalog-audit/testdata/` and a failing coverage test that lists uncovered UIDs (AC5)

## 2. Implementation

- [ ] 2.1 Add the Gemini API docs source, parser, and `ai.google.dev` trusted host; add its baseline entry (AC1)
- [ ] 2.2 Emit `source.inventory-unparsed` for inventory kinds (AC3)
- [ ] 2.3 Implement the numeric family-version rule and `catalog.family-pin-stale` (AC4)
- [ ] 2.4 Add `devin.yaml` entries for non-Fusion uncovered UIDs with reasoning levels from the CLI; include the deepseek-v4-pro UIDs (AC6)
- [ ] 2.5 Reconcile the Devin Haiku alias with `anthropic/claude-haiku-5-5`; update the modelpolicy selector test (AC6, AC8)
- [ ] 2.6 Add the Gemini API docs URL to `gemini.yaml` evidence refs and add `gemini-4-argon` to Devin's catalog (AC6)
- [ ] 2.7 Confirm `summary` and `propose` accept the new finding codes with schema 1 (AC7)

## 3. Verification

- [ ] 3.1 `go test ./tools/model-catalog-audit -count=1`, `go test ./pkg/harness-compilation/modelpolicy -count=1`, `go vet ./tools/model-catalog-audit` (AC8)
- [ ] 3.2 Live `go run ./tools/model-catalog-audit check` shows the Gemini, Devin, and family-pin findings (AC8)
- [ ] 3.3 Annotate each scenario with `// spec: model-catalog-audit/<slug>` and pass `go run ./tools/specaudit trace`

## 4. Blocked

- [ ] 4.1 BLOCKED: Cursor model inventory (AC3, Cursor). Source decision pending from the user. Local `cursor-agent` has no model-list command; `ghcr.io/adyen/agent-marketplace/harness-cursor:ci` is restricted on this host (administrator-gated registry).
