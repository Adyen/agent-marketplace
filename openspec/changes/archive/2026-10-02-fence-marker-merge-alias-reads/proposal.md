## Why

Marker-merge JSON migration reads an existing output symlink before proving the target's scope. A repository alias can cause foreign handwritten JSON to be copied into a checkout. Static root admission and atomic rename do not authorize that payload read.

## What Changes

- Admit existing marker alias targets before any payload read for mutation or checking.
- Read one captured regular, singly linked generation and retain its permissions; refuse escaping, dangling/cyclic or unsafe object targets.
- Preserve contained repository/HOME aliases, admitted machine output relocation and legacy managed-cache migrations, handwritten content and repeat-sync no-ops.

## Capabilities

### Modified Capabilities

- `materialization`: marker alias target read authority and refused-read preservation.

## Impact

Generic marker-merge input preparation, with no change to native durable-ownership aggregates. No new absolute file or aggregate budget; growth against the captured input generation refuses. Publication/cleanup, direct regular-file merge reads, source/support readers and manifest/restore consumers remain separate work.
