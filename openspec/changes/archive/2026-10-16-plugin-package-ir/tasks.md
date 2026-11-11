## 1. Plugin IR

- [x] 1.1 Append `ir.KindPlugin` after `ir.KindRule` and add the plugin IR
  (design D1) with a format-specific name grammar. Revise
  `pkg/harness-compilation/AGENTS.md` and add an ADR under `docs/adr/` in
  the same commit

## 2. Package decoders

- [x] 2.1 Add the `claude-plugin`, `factory-plugin`, and `codex-plugin`
  package decoders, with source locations, duplicate-key detection, bounded
  reads, and contained-file-set validation (escapes, cycles, special files,
  case-fold collisions)
- [x] 2.2 Add the Agent Plugins 1.0.0 decoder with bundled schema data; write
  red tests for every `plugin-portable-format` import scenario

## 3. Projection

- [x] 3.1 Add `pluginpolicy.Project` and the public package compile entry
  point (design D3); `Compile` on a plugin returns
  `plugin.compile.package-required`. Write red tests for the
  `plugin-portable-format` MCP runtime-value scenarios and the
  `plugin-delivery` restriction and preference scenarios
- [x] 3.2 Add the Claude Code `surfaces.plugin` entry blocking subagent
  `permissionMode`

## 4. Conversion fixtures

- [x] 4.1 Add `pluginpolicy/testdata/conversions.yaml` and its runner through
  the public package compile path in `make catalog-check`; annotate
  `plugin-policy-catalogs/a-plugin-fixture-mismatch-fails-the-check`

## 5. Gates

- [x] 5.1 Run `make catalog-check`, `make spec-check`, and `make validate`
