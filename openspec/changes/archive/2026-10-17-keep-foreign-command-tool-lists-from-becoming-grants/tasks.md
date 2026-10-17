## Implementation

- [x] Add public regressions for the two command scenarios, including a Marketplace payload round trip, and confirm the omission scenario fails on the prior code.
- [x] Add `ir.Command.ToolSource`, set it in Devin, Factory and Gemini command ingestion, keep an extension field from supplying it in the canonical payload, and keep it out of the frozen source-revision fallback digest.
- [x] Omit foreign command tool lists in the Claude, Cursor and Gemini command compilers.
- [x] Update the field matrix and changelog.
- [x] Run affected packages, `make validate`, `make spec-check` and `make validate-e2e`; review every changed entry adversarially.
- [x] `openspec archive keep-foreign-command-tool-lists-from-becoming-grants --yes`
