## Context

Portable digests control replacement/adoption/pruning ownership. Their regular-file branch independently opens the path after Lstat and trusts multiply linked payloads. Actual native Factory file-mode update accepted such ownership on parent and replaced installed output.

## Decisions

Reuse captured parent/destination streaming and full FileInfoIdentity to refuse replacement between observation and capture. A captured link where a regular leaf was observed also refuses. Digest the legacy header using captured mode/size and stream bytes at captured size through the stable opened-file callback. Retain errors from cleanup; no new absolute input/tree/work policy and no whole-file buffering. Existing directory/symlink encoding and comments stay unchanged. Directory capture/enumeration and mutation/prune generations remain tracked for later layers.

## Verification

Native Run positive install then hardlink/update is red/green: materialize.write.error, shared digest ErrUnsafeFile, unchanged native/external bytes/identity/mode/mtime, unchanged manifest bytes and no native staging residue. Independently authored SHA/header encoding protects compatibility; a deterministic post-observation replacement refuses ErrGenerationConflict before hashing any replacement bytes. Linux/macOS portable controls/full short materialize and selected count20 races, affected validate/lint/build and strict specs pass. All nine implementation entries reviewed including test/benchmark/script/spec exclusions with none skipped; regular observed/captured FD identity, no-follow/single-link/stream/state policy, byte encoding, errors, cleanup and caller disposition checked. Original source comments retained. Regular-leaf disk digest microbenchmark includes local IO/no network,2s ×5 arm64 median23.366→190.157µs(+713.82%),3851→9592B/op(+149.08%),14→86alloc(+514.29%),p=.008 n5/no95%interval. Validation cost, not lifecycle benchmark or speedup. Directory enumeration/mutation/pruning and final integrated R2/GitHub comments remain pending.
