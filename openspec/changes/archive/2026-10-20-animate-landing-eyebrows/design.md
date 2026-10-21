## Context

Landing now has a shared visual system but still lacks the approved motion
patterns. Existing tabs also need complete keyboard behavior before a moving
selection indicator can safely reflect their state.

## Goals / Non-Goals

**Goals:**
- Keep real content and accessible labels stable during animation.
- Reuse component state for copy and tab feedback.
- Disable movement, not functionality, for reduced motion.

**Non-Goals:**
- Add scroll hijacking, parallax, or repeated motion.
- Change commands, destinations, or default harness selection.

## Decisions

Motion is expressed through CSS and focused Vue composables. Eyebrow noise is
aria-hidden while real text remains in the DOM. Copy success is announced by a
polite status and only follows a resolved clipboard write. The decorative tab
indicator measures the selected tab after fonts and resize; selection remains
owned by the tab state. Every observer, animation frame, and timer is cleaned
up on unmount.

## Risks / Trade-offs

Font loading and wrapped tabs can change measurements. The indicator places
without animation after fonts or resize and animates only user selection.

## Migration Plan

Add the interaction primitives and browser witnesses, verify reduced motion,
and archive the change.
