## 1. Tests

- [x] 1.1 Add a Go test where a reload inserts rows above a selected lower header in a list that fits, annotated with `// spec: tui-navigation/reload-with-room-for-every-row-shows-the-whole-list`, and make the stage anchoring test use an overflowing list

## 2. Implementation

- [x] 2.1 Reset the list offset to the top in `clampListOffset` when every row fits

## 3. Journey, documentation and archive

- [x] 3.1 Add the `reload-shows-every-row` TUI QA journey with its README entry, and verify `make spec-check`
- [x] 3.2 Run `openspec archive tui-fitting-list-shows-every-row --yes`
