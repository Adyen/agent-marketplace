## Context

The registry filter currently renders six initial options plus any selected options before its expansion action. The sixth row pushes that action below the visible area in the reported desktop layout.

## Goals / Non-Goals

**Goals:**

- Move the expansion action one row earlier.
- Preserve selected-option visibility and full expansion.

**Non-Goals:**

- Change sticky scrolling, option dimensions, or mobile-specific layout.

## Decisions

Reduce the existing initial registry limit from six to five. This changes no component structure or scrolling behavior and keeps the action in normal document flow.

## Risks / Trade-offs

- [One fewer registry is visible initially] → The adjacent expansion action exposes all registries with one click.

## Migration Plan

Deploy the constant and test update together; rollback restores the previous limit.
