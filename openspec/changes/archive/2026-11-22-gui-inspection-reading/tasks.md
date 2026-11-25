## 1. Spec

- [x] 1.1 Add `gui-shell` requirements "Formatted Markdown in resource inspection" and "Dialogs clear the macOS window controls".

## 2. Implementation and witnesses

- [x] 2.1 Render Markdown with `markdown-it` in `gui/src/markdown.ts`, with frontmatter split and link, image, and HTML handling.
- [x] 2.2 Add the Formatted/Source control, the footer change, and the macOS modal offset.
- [x] 2.3 Add unit, component, and Playwright witnesses bound to the scenario slugs.
- [x] 2.4 Capture QA screenshots in light and dark, Source view, and a narrow macOS window.

## 3. Verify

- [x] 3.1 Run the affected GUI vitest files, the full GUI vitest suite, `vue-tsc --noEmit`, the touched Playwright inspection specs, and `make spec-check` from the repository root.
