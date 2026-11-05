## 1. Exporter

- [x] 1.1 Pure Agent Plugins exporter with loss and restriction reporting
  (design D1, D3, D4) and the `plugin.export.*` diagnostics; annotate the
  re-import and restriction scenarios

## 2. CLI

- [x] 2.1 `export plugin/<name> --format --out --allow-loss` reading the
  exact locked plugin with all-or-nothing output (design D2, D5); add
  behavioural witnesses for the command and every flag so
  `TestPublicCommandPathsHaveBehaviorTests` passes; annotate the loss,
  allow-loss, and non-empty directory scenarios
- [x] 2.2 Add the export path to the plugin process-spawn guard
  (`plugin-packages/plugin-lifecycle-runs-no-package-process`)

## 3. Gates

- [x] 3.1 Run `make validate` and `make spec-check`
