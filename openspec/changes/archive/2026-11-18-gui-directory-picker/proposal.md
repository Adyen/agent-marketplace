# Native folder picker for directory sources

## Why

Adding a directory registry in the desktop GUI currently requires typing or pasting its path. A native folder picker lets users find the directory without leaving the form.

## What Changes

- Offer Browse next to Location for directory registries in the native desktop app. Choosing a folder fills the field without adding a source; cancelling leaves it unchanged.
- Keep typed entry, validation, and the separate Add source action. Do not offer Browse for Git, Marketplace, or non-native sessions.

## Capabilities

### Modified Capabilities

- `gui-configuration`: Add native selection of directory registry locations.

## Impact

The shared source form and Wails desktop binding change. No changes to the local HTTP API, configuration schema, or source admission rules.

## Release note

In the desktop app, use Browse next to Location to pick a directory registry folder instead of typing its path.
