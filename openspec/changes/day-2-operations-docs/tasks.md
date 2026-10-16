## 1. Pages

- [x] 1.1 Land the seven new pages, sidebar entries, cross-links, and the configuration reference correction.
- [x] 1.2 Rewrite the sections that described the five fixed behaviors so they describe the fixed behavior.

## 2. Output

- [x] 2.1 Build a binary from `main` plus the four fix branches and rerun the day-2 experiment scripts in a Linux container.
- [x] 2.2 Replace every command output block on the changed pages with the matching fixed-binary output.

## 3. Verify

- [x] 3.1 Run `pnpm run test:public`, `pnpm run test:docs`, and the dead-link build.
- [x] 3.2 Run `openspec validate --all --strict`.
- [ ] 3.3 Archive with `openspec archive day-2-operations-docs --yes`.
