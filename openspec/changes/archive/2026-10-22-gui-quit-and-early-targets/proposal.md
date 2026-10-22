## Why

The GUI asks for quit confirmation even when nothing would be lost, and the
target harness list appears late because it waits for catalog acquisition.

## What Changes

- Quit exits immediately when there are no pending changes and no running or
  requested operation; the confirmation remains for pending or running work.
- The configuration snapshot stage reports the configured target harnesses as
  soon as the configuration layers validate, before acquisition finishes.
