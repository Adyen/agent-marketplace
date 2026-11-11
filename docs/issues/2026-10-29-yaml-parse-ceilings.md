# YAML Parse Ceilings (#434: R31 with R04/R32)

Decision: D1 option A, approved in the [#434 memo](https://github.com/Adyen/agent-marketplace/issues/434#issuecomment-6519245552). The ceilings are 64 KiB for frontmatter and 256 KiB for collection manifests, with no override.

## Problem

yaml.v3 checks duplicate mapping keys in O(N²) per mapping. No upstream release fixes this. A single resource's frontmatter, or a single `COLLECTION.yaml`, could therefore stall ingestion and every command that loads the registry.

## Change

| Consumer | Before parsing | Refusal |
| --- | --- | --- |
| `yamlfm.ParseYAMLFrontmatter` (skills, commands, subagents, rules, Factory/Gemini/Windsurf/OpenCode ingestion) | Block between delimiters > 64 KiB | `ingest.frontmatter.too-large` (coded error wrapping `ErrFrontmatterTooLarge`) |
| `bundle.LoadRule` lenient fallback | Never runs after a ceiling refusal | Same refusal |
| OpenCode ingestion diagnostics | Keeps the ceiling code instead of the generic invalid-resource code | `ingest.frontmatter.too-large` |
| `internal/skill` discovery | Accumulated block > 64 KiB | Malformed skill; error wraps `bundle.ErrFrontmatterTooLarge` |
| `internal/collection` discovery | Manifest > 256 KiB | `ErrManifestTooLarge` |

Siblings in the same source still load. The ceilings are measured on the YAML bytes, so the body and support files are unaffected.

## Evidence

Apple M4 Max, macOS, Go 1.27.3; the host was loaded. Raw data is in [yaml-parse-ceilings-2026-10-29](yaml-parse-ceilings-2026-10-29/).

`BenchmarkParseYAMLFrontmatterKeys` parses a single mapping of unique keys, the worst case for the duplicate-key check. Five samples per side:

| Frontmatter | Before | After |
| --- | ---: | ---: |
| 2 KiB | 132.0 µs | 131.6 µs (~) |
| 64 KiB (largest accepted) | 41.8 ms | 42.6 ms (~) |
| 1 MiB | 13.12 s | 0.28 ms (refused) |

A probe on the collection-manifest decoder parsed a 256 KiB mapping in 0.62 s, the worst accepted manifest.

A local registry-cache sample shows real sizes far below both ceilings:

| Measure | Value |
| --- | ---: |
| Frontmatter blocks sampled | 1,700 |
| Median frontmatter | 261 B |
| p99 frontmatter | 1,008 B |
| Largest frontmatter | 2,023 B |
| Largest collection manifest | 5,363 B |

## Limits

- The whole file is still read before the check, so memory use is unchanged and this limit is not a read-size budget. Read-size ceilings stay in #988.
- The library collection graph (R29, R30) is out of scope.
