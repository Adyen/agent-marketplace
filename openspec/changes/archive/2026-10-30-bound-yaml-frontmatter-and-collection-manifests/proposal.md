## Why

yaml.v3 compares every mapping key with every later key, so parsing time grows quadratically with the number of keys. A registry contributor can stall ingestion and every command that loads the registry: 1 MiB of skill frontmatter takes 12.9 s to parse, and a collection manifest goes through the same decoder (#434, R31 with R04/R32). Real frontmatter is a few kilobytes (largest observed 2,023 B) and real collection manifests are a few kilobytes (largest observed 5,363 B).

## What Changes

The maintainer-approved D1 decision for #434 adds two ceilings, with no override:

- YAML frontmatter blocks above 64 KiB are refused before parsing, with the stable diagnostic `ingest.frontmatter.too-large`. This covers skills, commands, subagents and rules, including the rule parser's lenient fallback. CLI skill discovery reports such a skill as malformed.
- `COLLECTION.yaml` manifests above 256 KiB are refused before parsing.

Other resources in the same registry still load. The worst accepted parse is about 0.05 s for frontmatter and 0.6 s for a manifest on the measured host.

## Release note

Resources whose YAML frontmatter exceeds 64 KiB, and collection manifests larger than 256 KiB, are now refused instead of being parsed. This prevents oversized registry content from stalling commands.

## Capabilities

### Modified Capabilities

- `harness-compilation`: YAML frontmatter parse ceiling.
- `catalog-discovery`: skill frontmatter and collection manifest parse ceilings.

## Impact

Affected: `pkg/harness-compilation/internal/yamlfm`, the `bundle` rule loader, `internal/skill` frontmatter discovery, `internal/collection` manifest discovery, and one new diagnostic code.

This is a new D1 limit, approved in #434. It adds no dependency and no override.
