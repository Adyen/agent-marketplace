## Why

The "Audit upstream model catalogs" job (run 41961800213 on main) failed on a read-only audit that ran correctly but cannot see Gemini 4 Argon, does not compare Devin's concrete model inventory, and does not flag family pins that trail the newest observed model. The job also fails on documentation-only drift, which buries real model signal.

## What Changes

- Add a Gemini API model-documentation inventory source, with a parser that extracts model IDs from its page, so Gemini 4 Argon is compared against catalogs that cite the page.
- Emit `source.inventory-unparsed` naming a source whose page yields no parseable model list, instead of passing it silently.
- Emit `catalog.family-pin-stale` when a family pin names a model older than the newest observed member of that family in the same source's inventory.
- Snapshot `devin models list --format json` and test that every `model_uid` is covered by a Devin catalog entry, listing uncovered UIDs.
- Add Devin catalog entries for the non-Fusion UIDs the snapshot reports as uncovered, and reconcile the Devin Haiku alias with the CLI.
- Add the Gemini API documentation URL to the Gemini catalog evidence references, and add Gemini 4 Argon to the providers whose published inventory lists it.
- Leave the Cursor model inventory open: it is recorded as a blocked task until the user decides its source.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `model-catalog-audit`: adds the Gemini API documentation inventory, inventory-unparsed findings, family-pin staleness, and Devin inventory coverage.

## Impact

- `tools/model-catalog-audit/` (sources, extraction, audit, tests, testdata fixtures, baselines for the new source).
- `pkg/harness-compilation/modelpolicy/catalogs/devin.yaml`, `gemini.yaml` (evidence ref only), and `devin_catalog_test.go`.
- No CLI, API, or harness output behavior changes; the audit report schema stays at version 1.
