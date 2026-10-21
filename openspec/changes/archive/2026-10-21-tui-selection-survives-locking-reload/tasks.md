## 1. Tests

- [x] 1.1 Add Go tests for list selection (unique and ambiguous identity) and an open detail view through a reload that changes the catalog key, annotated with `// spec: tui-navigation/selection-survives-a-reload-that-locks-its-source`, and reproduce the jump in a real terminal

## 2. Implementation

- [x] 2.1 Record the resource identity in rebuild anchors and fall back to the unique matching row for list selection and the open detail view; verify the reproduction keeps `alpha-skill` selected

## 3. Journey, documentation and archive

- [x] 3.1 Make `org-marketplace-lock-failure` assert the settled state after its local toggle, update its README entry, and verify `make spec-check`
- [x] 3.2 Run `openspec archive tui-selection-survives-locking-reload --yes`
