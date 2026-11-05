## Why

Archive verification applied the archive limits (8 MiB payload, 2 MiB per asset, license or support file, 256 asset and 256 support files, 1024 files and 64 MiB in total) only after the whole archive was decoded. Decoding itself capped each member at 64 MiB but kept no running total and no entry count, so a small, highly compressed archive could expand into many 64 MiB members before verification refused it (R61 in the R5 ledger, finding F172). The Marketplace client verifies archives it downloads, so content the user did not write could make the user's machine decompress and hold far more than the archive's size.

## What Changes

- Verifying an archive by digest, verifying a final archive against its placeholder, and decoding an archive for content enforce the existing archive limits while decoding: per-member sizes are checked against the declared size before the member is read, file counts and decoded bytes are totalled as members arrive, and decoding stops at the first member that exceeds a limit.
- Entries the canonical layout never writes are bounded too: archive entries beyond the 1024 files plus the manifest and its four directories, members outside the layout directories and a repeated manifest are refused, and the manifest and members outside the layout directories count toward the total bytes.
- Limit refusals wrap the exported sentinel `ErrArchiveLimitExceeded`, and the total is compared without integer overflow, so a declared size near the maximum integer cannot wrap it.
- No limit changes value. Archives the encoder produces within the limits verify as before; the unchecked `Decode` keeps its behavior.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-api`: verified archive decoding stops at the archive limits.

## Impact

`pkg/harness-compilation/artifact` verification functions used by the Marketplace client and server. Over-limit archives are refused earlier with a limit error; within-limit archives are unaffected. No API, lock, or configuration change.
