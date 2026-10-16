## Context

See `proposal.md` for motivation. VitePress and Nuxt both import `design/brand.css`, but own their navigation markup independently. Landing section labels flow through `SectionShell`; Marketplace catalog rows already render a semantic registry/name coordinate.

## Goals / Non-Goals

**Goals:**
- Establish one restrained cross-surface brand treatment.
- Separate local Landing documentation navigation from canonical Marketplace documentation navigation.
- Preserve accessible names and resource-coordinate semantics.

**Non-Goals:**
- Change documentation routes or remove Marketplace-hosted `/docs/`.
- Apply the slash to body copy, paths, commands, breadcrumbs, admin screens, or documentation content.
- Redesign catalog layout or resource-detail presentation.

## Decisions

- Keep one shared 160px logo token so all browser surfaces remain aligned.
- Add a dedicated canonical Marketplace Docs URL rather than changing the local `DOCS_PATH` used elsewhere.
- Render the Marketplace Docs action as a native isolated new-tab link with the existing Lucide external-link icon.
- Define one shared weight-800 `#00D16A` slash class. Decorative instances are hidden from assistive technology; the coordinate delimiter remains accessible.
- Author spaces directly around slash characters instead of simulating them with CSS margins; preserve compact accessible coordinate names explicitly with `aria-label`.
- Limit decorative placements to Landing section eyebrows and the Marketplace instance title. Primary-color eyebrow text increases contrast without making functional headings decorative.
- Apply the decorative title slash only to skills and commands, where the prefix reinforces their authored tool identity without adding noise to collections or other resource kinds.
- Draw coordinate hover emphasis with one inset link-level line so font-relative slash margins do not fragment it or place it outside the clipped row title.
- Verify external navigation through attributes rather than making tests depend on the public network.

## Risks / Trade-offs

- [Canonical documentation is unavailable] -> The isolated tab preserves the current Marketplace page.
- [The slash motif becomes repetitive] -> Approved placements exclude hero, catalog utility headings, and lower-level UI.
- [Decorative text changes accessible names] -> Decorative slashes use `aria-hidden`; exact accessible names remain covered.
- [Coordinate styling changes identity] -> Adjacent spans preserve the literal accessible `registry/name` value and route.
