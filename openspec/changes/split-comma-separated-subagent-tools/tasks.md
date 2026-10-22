## 1. Implementation

- [x] 1.1 Read a subagent `tools` or `disallowedTools` string as a list in `bundle.LoadSubagent`, keeping a value that is not well formed as the single entry earlier versions produced, with unit tests and a benchmark
- [x] 1.2 Add public conversion regressions annotated `// spec: harness-compilation/<slug>` for Claude and Cursor sources and for the not well formed fallback, and confirm the splitting ones fail on the prior code
- [x] 1.3 Add declarative e2e rows that sync a string-form subagent for Claude, Factory and Gemini and keep a malformed string installed as one entry
- [x] 1.4 Record the change in the harness-compilation changelog

## 2. Archive

- [ ] 2.1 Run `openspec archive split-comma-separated-subagent-tools --yes`
