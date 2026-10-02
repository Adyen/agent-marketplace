## Context

The primary navigation is sticky at the viewport top and the desktop filter rail stops at 88px. Search, result count, active-filter chips, and metadata warnings currently continue scrolling with resource rows.

## Goals / Non-Goals

**Goals:**

- Let the introduction scroll away naturally.
- Keep catalog controls and filters aligned at the 88px sticky offset.
- Keep resource rows in normal document scrolling.

**Non-Goals:**

- Create an independently scrolling resource pane.
- Change mobile layout or filter scrolling.

## Decisions

Wrap search, mobile-filter markup, the Resources heading/count, active-filter chips, live status, and metadata warning in one `catalog-results__controls` container. On desktop only, make the wrapper sticky at 88px with an opaque surface background and a z-index below the primary navigation. Resource loading, empty, error, and result content remain outside the wrapper and continue through normal page scrolling.

A single wrapper prevents dynamic active-filter rows or warnings from overlapping resources, unlike independently sticky child elements.

## Risks / Trade-offs

- [A tall warning or many active-filter chips reduce the visible rows] → Keep all controls in the sticky wrapper so their actual height is reserved in document flow and rows never render underneath them unintentionally.

## Migration Plan

Deploy the wrapper, desktop CSS, and browser regression together. Rollback removes the wrapper positioning while retaining its semantic grouping.
