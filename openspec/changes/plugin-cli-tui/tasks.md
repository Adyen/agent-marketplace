## 1. CLI lifecycle

- [ ] 1.1 `list --kind plugin`, `inspect plugin/<name>` (design D1),
  `install`, `remove`, `update`, `sync`, `sync --check`, and `status`
  (locked versus installed revision). Add behavioural witnesses for every
  new command path and flag so `TestPublicCommandPathsHaveBehaviorTests`
  passes; annotate the CLI lifecycle scenarios
- [ ] 1.2 Decode declared dependencies and refuse them at sync with
  `plugin.format.dependencies-unsupported` (design D2); annotate
  `plugin-packages/plugin-with-dependencies-is-refused`

## 2. TUI

- [ ] 2.1 TUI parent rows with member rows that toggle exclusion (design
  D3); add TUI QA journeys and annotate the TUI scenarios

## 3. No-execution boundary

- [ ] 3.1 Verify no subprocess runs during the plugin lifecycle with a
  process-spawn guard (design D4); annotate
  `plugin-packages/plugin-lifecycle-runs-no-package-process`

## 4. Gates

- [ ] 4.1 Run `make validate`, `make spec-check`, `make tui-qa`, and
  `make validate-e2e`
