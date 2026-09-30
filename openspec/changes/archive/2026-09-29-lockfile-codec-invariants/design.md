## Context

`internal/lockfile` already implements canonical encoding; existing golden
and normalization tests verify it end to end, and this PR's Rapid property
suite checks the same contract against generated and shrunk inputs. This
change records only the verified contract.

## Goals / Non-Goals

**Goals:** one `acquisition-lock-codec` capability with every scenario bound
to a `// spec:` annotation in an existing `internal/lockfile` test.

**Non-Goals:** no production changes; no specification of codec internals or
the property-test model as behavior. Claims are grounded in existing tests
(`TestMinimalDocumentsGolden`,
`TestCompleteGitDocumentGoldensCoverEveryScope`, `TestV2MarketplaceGolden`,
`TestNormalizeEncodeDeterministic`,
`TestMarketplaceEndpointAndSelectionOrderingDedup`,
`TestV2NormalizesNonCanonicalEndpointOnDecode`, `TestEncodeSizeBoundary`).

## Decisions

- The committed v1 golden bytes are specified as a codec compatibility
  fixture: v1 remains a read/write schema the codec accepts, while current
  CLI writers emit v2 only. No scenario claims the CLI emits v1.
- Decode scenarios concern reading existing (possibly non-canonical) YAML,
  not emitted bytes, so non-canonical spellings on disk canonicalize on read.
- Ordering and deduplication scenarios assert emitted bytes, proven by Encode
  byte assertions added to
  `TestMarketplaceEndpointAndSelectionOrderingDedup`.
- Identity-conflict rejection for colliding records is deliberately left
  unspecified: it is proven only by the new property suite, with no existing
  consumer or golden test asserting it.
- Other unverified nuances (size limits, diagnostic codes, decode
  strictness) remain unspecified.

## Risks / Trade-offs

- Byte-exact golden scenarios couple the spec to the current field order;
  that coupling is intended, since the committed bytes are the contract.
