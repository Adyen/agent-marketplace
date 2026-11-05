## Context

The public-site foundation supplies shared semantic roles. Landing components
still carry the previous visual system and duplicate terminal depiction styles.

## Goals / Non-Goals

**Goals:**
- Apply one restrained visual hierarchy across Landing.
- Consolidate repeated depiction styling.
- Preserve content, links, controls, and responsive usability.

**Non-Goals:**
- Add motion or change keyboard behavior.
- Change page-level information architecture or product copy.

## Decisions

Landing sections use one content edge and centralized spacing roles. Product
depictions share one flat terminal-screen stylesheet. Headings, actions,
technical labels, and supporting text map to the public-site type roles.
Existing component state remains authoritative.

## Risks / Trade-offs

The redesign touches most Landing sections. Production builds and the browser
witness cover preserved content, flat hero presentation, and shared typography.

## Migration Plan

Apply the visual system without interaction changes, verify both deployment
profiles, and archive the change.
