## 1. Spec

- [x] 1.1 Add `gui-shell` requirement "Resource rows show plain kind, scope, and harness labels" with its scenario.

## 2. Implementation and witnesses

- [x] 2.1 Show singular kind display names in the row kind column, and keep plural names for facets and section titles.
- [x] 2.2 Show Org, Project, and Local scope tags, and an outlined "Only <harnesses>" override tag.
- [x] 2.3 Add a Vitest witness bound to `gui-shell/resource-rows-show-plain-kind-scope-and-harness-labels`.

## 3. Verify

- [x] 3.1 Run the full gui Vitest suite, vue-tsc, the affected Playwright specs, and `make spec-check`.
- [x] 3.2 Capture an after screenshot of resource rows.
