## Why

`buildRows` created malformed rows with only a directory name and path. A malformed skill from a configured Git registry was therefore labelled `(local)/<name>`, its detail showed no registry or ref, and two same-named malformed skills from different registries shared one row key (#889). The label rule falls back to `(local)` only when registry metadata is missing, and here it existed.

## What Changes

- Malformed rows take the registry URL, display name, ref and path of the configured registry whose catalog key reported them, and record that catalog key.
- `tui-rendering` specifies that malformed rows keep their registry.
- A 50×12 TUI QA journey loads a registry with a skill whose description and body contain terminal control bytes and a skill folder whose name does. It checks the visible escapes in the list, the details and the full view, and the registry-qualified malformed label.

## Capabilities

### New Capabilities

### Modified Capabilities
- `tui-rendering`: malformed rows keep their registry.

## Impact

`internal/tui` row building (`buildRows`). Malformed rows from catalogs without a configured registry entry keep the `(local)` label.
