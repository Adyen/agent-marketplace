## Why

Sidebar Marketplace sources take their label from the configured source name. A user source keyed `marketplace` and an organization source keyed `adyen` for the same endpoint therefore show different labels, even though they are one Marketplace. The endpoint is the stable identity of a Marketplace source, so the sidebar should label it by endpoint.

## What Changes

- A sidebar Marketplace source is always labeled by its endpoint host (with port and last path segment when present), regardless of its configured name, its generic-name status, or its configuration layer.
- The source browse header shows the same endpoint label.
- Tooltip, accessible name, and contrast requirements are unchanged.

## Capabilities

### Modified Capabilities

- `gui-shell`: sidebar Marketplace source labels come from the endpoint, not the configured name.

## Impact

Desktop GUI source labels (`gui/src/source-label.ts`) and their tests. No configuration schema, local API, Marketplace server, TUI, or resource behavior changes. Source search still matches configured names.
