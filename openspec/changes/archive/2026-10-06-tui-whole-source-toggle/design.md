## Context

The original feature predates the single status view, Charm v2, validated workspace roots, and v4 writable configuration. Keep the current architecture and port the source operations rather than restoring the old tabs.

## Decisions

- Status sections retain their rows while folded. A section can open the menu only when those rows share a source.
- Source actions resolve across all resource kinds, not only filtered rows. Required resources remain protected.
- Git and directory edits run inside the existing dependency-aware staged mutation, retaining the real declaring path for includes and relative directories.
- Marketplace pin edits use their existing transaction path; organization exclusions use the staged user override path.
- New configuration roots use v4. Existing legacy roots keep their schema.
- Provisional inventory cannot authorize source mutations. The menu rechecks this on submission.

## Risks

Whole-source selection changes future resources too through `all: true`. Included selectors or exclusions that prevent the requested result are refused rather than silently rewritten. Mixed-source sections require selecting a resource explicitly.
