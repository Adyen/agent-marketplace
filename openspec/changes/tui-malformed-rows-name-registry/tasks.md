## 1. Tests

- [x] 1.1 Add a Go test for malformed rows from two configured registries (labels, metadata, distinct keys) and from an unconfigured catalog (`(local)`), annotated with `// spec: tui-rendering/malformed-rows-name-their-registry`

## 2. Implementation

- [x] 2.1 Fill malformed rows' registry fields and catalog key from the configured registry entry with the same catalog key

## 3. Journey, documentation and archive

- [x] 3.1 Add the `hostile-metadata` 50×12 TUI QA journey and its README entry, and verify `make spec-check`
- [ ] 3.2 Run `openspec archive tui-malformed-rows-name-registry --yes`
