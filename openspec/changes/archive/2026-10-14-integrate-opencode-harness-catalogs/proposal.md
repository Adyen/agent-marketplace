## Why

`main` moved model, tool, and hook translation for every harness target onto shipped per-harness catalogs (`modelpolicy`, `toolpolicy`, `hookpolicy`) and routes hooks through native hook aggregates with per-hook support directories. The OpenCode harness target was written against the removed mapping tables, so it no longer compiles, and its hooks would be suppressed by the aggregate pipeline it does not use. Integrating it also exposed two pre-existing defects: OpenCode checks `write` and `apply_patch` under the `edit` agent permission, so a translated `write` deny rule was never enforced, and the hook adapter does not report `apply_patch` as `Edit` or `Write`, so a file-edit hook translated to OpenCode could be bypassed by patch edits. Hook commands also pointed into the registry cache, which the native hook contract forbids.

## What Changes

- Ship OpenCode `modelpolicy`, `toolpolicy`, and `hookpolicy` catalogs grounded in the OpenCode documentation and the pinned native compatibility gate.
- Translate OpenCode subagent and command models through the model catalogs: canonical models without a catalog identifier report `model.unresolved`; native OpenCode selections keep their spelling through model provenance.
- Translate agent tool restrictions at OpenCode permission granularity and refuse lists that cannot be represented exactly.
- Translate hook events, matchers, deadlines, and command variables through the hook catalogs; a file-modification matcher must cover every OpenCode editing tool, including `apply_patch`.
- Keep OpenCode hooks as per-resource declarations beside the fixed adapter. Hook support files are not installed, so plugin-root and project-directory references are refused instead of pointing into the cache.
- Refuse ingested declarations whose matcher the adapter would read differently from catalog decoding.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `harness-opencode`: catalog-driven model and tool translation with permission-granular agent restrictions.
- `opencode-hooks`: catalog-driven hook translation, refusal of support-dependent commands, and unambiguous declaration matchers.

## Impact

OpenCode compiler, ingester, catalogs, and materialization routing for targets whose hooks are resource files. `toolpolicy` gains a concept lookup and `hookpolicy` exposes the shipped matcher syntax and registers the existing `hook.opencode.unsupported` code. Existing harness targets, acquisition locks, and the frozen legacy model table are unchanged.
