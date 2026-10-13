## Context

The horizontal SVG uses a 17-unit viewBox while its painted content sits slightly high within that box. At the shared 200px width, the screenshot shows the painted wordmark approximately one CSS pixel above the 64px navigation midpoint even though the image element itself is flex-centered.

## Goals / Non-Goals

**Goals:**
- Optically center the visible wordmark across Landing, Docs, and Marketplace.
- Keep alignment policy shared across light and dark variants.

**Non-Goals:**
- Modify the supplied SVG files or viewBoxes.
- Change logo width, wrapper geometry, header height, or neighboring controls.

## Decisions

- Add a shared 1px vertical offset brand token and translate each logo image by that amount. One pixel corrects the observed painted-content imbalance without changing document flow.
- Apply the transform at each frontend's existing logo selector so both light and dark images inherit it.
- Compare the visible images' computed transforms across all three browser surfaces and require a non-zero transform.

## Risks / Trade-offs

- [A future SVG may have different internal whitespace] → Keep the optical offset explicit in the shared brand token and covered by browser tests so a future asset update prompts deliberate review.
