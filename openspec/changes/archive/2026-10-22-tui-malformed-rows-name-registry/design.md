## Context

The catalog loader records each configured registry's catalog key on `cfg.Registries[i].CatalogKey`, and `buildRows` receives the loaded catalogs keyed by that same catalog key. Available and selected rows take their registry fields from the resolver, but malformed skills are not resolvable, so their rows were built directly from `cat.Malformed` without any registry fields.

## Goals / Non-Goals

**Goals:** malformed rows show their source like every other row and have distinct identities per source.

**Non-Goals:** recursive sub-bundle catalogs whose keys have no configured registry entry; those keep the `(local)` label, as before.

## Decisions

- **Look up by catalog key.** A map from catalog key to registry entry, built once per `buildRows`, fills the row's URL, display name, ref and path, and the catalog key goes on the artifact. This is the identity the loader already assigns, so no new matching rule is needed.
- **Nil-safe.** `buildRows` accepts a nil config elsewhere; the lookup is empty in that case, and the label falls back to `(local)`.

## Risks / Trade-offs

- [Row keys of malformed rows change] → they now include the catalog key and registry fields, as other rows' keys do. A selected malformed row may lose its selection across the one reload where the new binary first runs.
