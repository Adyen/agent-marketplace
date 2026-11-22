## Why

Inspected Markdown was unreadable raw text, the dialog had a duplicate close button, and it overlapped the macOS window controls.

## What Changes

- Markdown files open formatted, with frontmatter shown verbatim above the body and a Source view of the unmodified file.
- Formatting never executes or renders raw HTML, never loads images, and never navigates the window; only http and https links open, in the system browser.
- Resource inspection has a single close control in its header.
- On macOS every dialog clears the window controls.

## Capabilities

### Modified Capabilities

- `gui-shell`: formatted Markdown in resource inspection, and dialogs clear the macOS window controls.

## Impact

Desktop GUI inspection dialog, modal layout, and the Markdown renderer dependency (`markdown-it`). No configuration, local API, Marketplace, TUI, or resource behavior changes.

## Release note

Resource inspection now shows Markdown formatted, with a Source view for the raw file, and no longer overlaps the macOS window buttons.
