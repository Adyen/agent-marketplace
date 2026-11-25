## Why

A configured source is labelled one way in the sidebar and differently elsewhere: the filter chip, the source browse header title, and the inspector's Registry value can show a different name for the same source. The browse header also prints the full location, which wraps across several lines.

## What Changes

- The source filter chip, the source browse header title, and the inspector's Registry value use the sidebar's source label when the row belongs to a configured source.
- The browse header shows the location shortened to its last two path segments, with the full location in its title tooltip. Source actions still copy the full location.

## Capabilities

### Modified Capabilities

- `gui-shell`: source identity labels in the filter chip, browse header, and inspector.

## Impact

Desktop GUI source label helpers (`gui/src/source-label.ts`), the browse header, the filter chip, and the inspector Registry value. No configuration, local API, or resource behaviour changes.

## Release note

The desktop app shows the same name for a configured source in the sidebar, filter chip, browse header, and inspector.
