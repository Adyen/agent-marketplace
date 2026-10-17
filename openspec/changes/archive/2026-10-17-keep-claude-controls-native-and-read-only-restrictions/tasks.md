## Implementation

- [x] Add public regressions for the four scenarios and confirm the omission, mapping and refusal scenarios fail on the prior code.
- [x] Record Claude execution controls in the Claude sidecar on ingestion, and emit them on Claude only from there, with `compile.claude.native-control-omitted` for other sources.
- [x] Map read-only subagents on Claude, Cursor, Codex and Factory, refuse them on Gemini, Devin and OpenCode, and record the Factory `read-only` tools category.
- [x] Keep the frozen source-revision fallback digest basis unchanged.
- [x] Update the field matrix and changelog.
- [x] Run affected packages, `make validate`, `make spec-check` and `make validate-e2e`; review every changed entry adversarially.
- [x] `openspec archive keep-claude-controls-native-and-read-only-restrictions --yes`
