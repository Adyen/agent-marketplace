## Context

An inline block with the default `vertical-align: baseline` aligns its bottom margin edge to the surrounding baseline. Because the slash is `0.8em` high and has no descender, it extends entirely upward and sits higher than adjacent cap-height text.

## Goals / Non-Goals

**Goals:**
- Align the slash optically with adjacent cap height and baseline.
- Preserve proportional behavior at every local font size.

**Non-Goals:**
- Change slash height, angle, stroke, or spacing.
- Add context-specific vertical offsets.

## Decisions

Set `vertical-align: -0.08em` on the base `.am-brand-slash`. The negative font-relative value lowers the mark so roughly `0.72em` remains above the baseline and `0.08em` extends below it. Applying this once to the shared primitive keeps every placement consistent.

## Risks / Trade-offs

The offset is optical rather than derived from a specific font's metrics. A shared `em` value is preferable because the mark appears beside multiple fonts and sizes, and browser coverage verifies proportional scaling.

## Migration Plan

Add the shared baseline declaration and assertions. No markup or data migration is required.

## Open Questions

None.
