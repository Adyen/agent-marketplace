## 1. Tests

- [x] 1.1 Add a Go test that opens the project editor with `E`, `p`, captures the exec callback, and checks that its message reloads and frees the operation serializer, annotated with `// spec: tui-rendering/editor-exit-reloads-and-frees-writes`

## 2. Implementation

- [x] 2.1 Pass the editor operation into `openEditorCmd` for both results, remove the unreachable `correlateOperation` case, and reproduce the fix in a real terminal

## 3. Journey, documentation and archive

- [x] 3.1 Add the `edit-config` TUI QA journey with a scripted `$EDITOR` and its README entry, and verify `make spec-check`
- [ ] 3.2 Run `openspec archive tui-editor-exit-reloads --yes`
