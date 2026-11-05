## Why

Searching is the main way to use the Marketplace catalog, but reaching the search input requires the mouse or several Tab presses. `/` is the common shortcut for focusing search on sites such as GitHub, and the input gives no hint that a shortcut exists.

## What Changes

- The catalog page registers a `/` shortcut that focuses the search input. The shortcut is ignored while a text field has focus, so `/` can still be typed into a query.
- The search input shows a `/` keyboard hint in its trailing slot and declares `aria-keyshortcuts="/"`.
- `marketplace-web-ui` adds "Catalog search keyboard shortcut".

## Capabilities

### New Capabilities

### Modified Capabilities
- `marketplace-web-ui`: the catalog search input can be focused with `/` and shows that hint.

## Impact

`web/app/pages/index.vue` and the web Playwright suite. Search behavior and URLs are unchanged.
