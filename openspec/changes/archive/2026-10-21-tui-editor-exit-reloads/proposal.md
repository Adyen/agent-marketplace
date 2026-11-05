## Why

After a user edited a config layer with `E`, the TUI resumed without reloading, and every later config write in that session was silently refused (#869). `openConfigEditor` serializes the editor as an operation and relied on `correlateOperation` to stamp the operation on the result. The result is emitted by Bubble Tea's exec callback after the editor exits, not by the command itself, so it carried operation 0, `Update` dropped it, no reload ran, and the editor operation never finished, which blocked every toggle, harness save and other serialized operation until restart. The Go test built the result with the operation already set, so it never covered the real path.

## What Changes

- `openEditorCmd` takes the editor operation and puts it on both results it can emit: the exec callback's exit message and the directory-creation error. The now-dead `editConfigPathMsg` case in `correlateOperation` is removed.
- A package variable for `tea.Exec` lets a Go test capture the exit callback through the real `E`, `p` key path.
- `tui-rendering` specifies that the editor exit reloads the configuration, reports failures with recovery guidance, and leaves later writes working.
- A TUI QA journey drives `E` with a scripted `$EDITOR`: a failing editor, then a successful edit that appears without `r`, then a toggle that still writes.

## Capabilities

### New Capabilities

### Modified Capabilities
- `tui-rendering`: the editor handoff now specifies the post-exit reload, failure reporting, and later writes.

## Impact

`internal/tui` editor command (`edit_config.go`) and operation correlation (`requests.go`).
