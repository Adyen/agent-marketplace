## Context

`buildOccurrences` deduplicates not-found reports by (kind, name, URL, path, ref) so each miss is reported once. The first entry to hit the key creates the error; later entries only skip. Because `config.MergeLayers` keeps one entry per layer, an optional entry from an earlier layer can create the error before a required entry from a later layer for the same location is seen.

## Decisions

- Store the reported error per key. When a later entry for an already-reported key is required, upgrade that same error in place (`markSelectionRequired`). The error pointer is shared by the direct and deferred collection slices, so no further plumbing is needed.
- The TUI row sets `UnavailableSoft` to `soft && !required`, so the row follows its merged entry's required bit.

## Alternatives Considered

- Reporting a second error for the required entry: rejected because the spec requires exactly one not-found diagnostic per resource.
- Merging required status in config layering: rejected as broader than needed; the resolver is the only place that reads the miss severity.
