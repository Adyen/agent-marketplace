## Why

Archive extraction and Codex validation reopen archive payload paths without opened-file shape/generation authority. Actual Factory restore accepts a multiply linked archive and overwrites current content with its payload.

## What Changes

- Share captured regular/single-link/nonblocking archive payload admission across extraction and Codex preflight.
- Stream gzip/tar from the shared snapshot reader bounded at captured compressed-file size with opened/named state validation.
- Preserve valid extraction/native mappings and existing archive semantics; creation/source payload reads and restore writes remain separate next consumers.

## Capabilities

### Modified Capabilities

- `backup-recovery`: archive payload admission before decoding/extraction.

## Impact

R2 child of #500. No arbitrary new compressed/expanded byte budget; not a transactional extraction or expansion-budget fix. Output publication/creation/cleanup and ownership evidence stay tracked.
