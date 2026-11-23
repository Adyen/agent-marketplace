## 1. Spec

- [x] 1.1 Add `gui-shell` MODIFIED requirement "Sidebar sources open their Settings entry" and ADDED requirement "Add source modal" with its scenarios.
- [x] 1.2 Add `gui-configuration` MODIFIED requirement "Add-source errors stay in the Add source section".

## 2. Implementation and witnesses

- [x] 2.1 Add `gui/src/AddSourceModal.vue`, wired from the sidebar "+", the empty workspace, and Settings → Sources.
- [x] 2.2 Remove the inline add form from Settings → Sources and add its "Add source" button.
- [x] 2.3 Keep the modal open while pending and close only when the add settles without a failure; show the failure inline on failure.
- [x] 2.4 Track the add baseline in `controller.ts` so Settings does not repeat the modal's failure.
- [x] 2.5 Bind the scenarios with `// spec:` annotations in `add-source-modal.test.ts`, `sources.test.ts`, and the Playwright sources specs.
- [x] 2.6 Migrate the sources, gui, and accessibility Playwright specs to the modal flow; first-run keeps its inline form.

## 3. Verify

- [x] 3.1 Run the full gui vitest, vue-tsc, and the sources, gui, and accessibility Playwright specs on rebuilt dist.
- [x] 3.2 Update `gui/DESIGN.md` and capture after screenshots.
- [x] 3.3 Run `make spec-check`, then archive the change.
