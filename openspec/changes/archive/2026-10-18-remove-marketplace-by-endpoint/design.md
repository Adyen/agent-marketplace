## Decision

Treat a configured Marketplace endpoint URL as the source identity for named
`--from` removal. After validated effective configuration loads, and before
Git or directory URL validation, match the URL against configured Marketplace
entries with the existing endpoint normalization. A match narrows removal to
those entries, preferring the requested scope, and reuses the existing
Marketplace pin and collection removal unchanged. No match leaves Git and
directory handling unchanged.

The ambiguity remediation for a Marketplace candidate prints only `--scope`
and `--from`. Installed names already distinguish pins within one endpoint, so
the registry ID is not overloaded onto `--path`.

Without `--scope`, endpoint removal targets repository scope, matching the
documented `--from` default; existing scope-boundary refusal applies when the
pin lives elsewhere. `--ref` and `--path` have no Marketplace meaning and are
refused rather than ignored. A URL that also identifies a configured Git or
directory source is ambiguous and refused before any write.

A Marketplace-specific flag was rejected because the refusal already uses
`--from` as its source disambiguator and a new public flag would need its own
command surface.

## Verification

Run the emitted remediation for both the Marketplace pin and the local source,
with healthy and missing local resources, asserting exact selections and lock
contents. Cover the repository default, explicit user scope, and refusal for a
user-only pin. Refusals for `--ref`, `--path`, missing name or kind, and a
registry alias must leave configuration and acquisition locks unchanged. A
Marketplace E2E executes the suggested command through the public CLI.
