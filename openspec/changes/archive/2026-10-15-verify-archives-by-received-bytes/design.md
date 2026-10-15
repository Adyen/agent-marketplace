## Context

A canonical archive has two encodings: the placeholder form (manifest
`archive_digest` set to a fixed placeholder) and the final form (manifest
`archive_digest` set to the placeholder-form digest). Only the placeholder form
is hashed. Verifiers historically rebuilt the placeholder form by re-encoding,
which couples verification to the Go compressor.

## Decisions

- **Hash what is held.** A placeholder archive is verified by hashing its bytes
  against the digest. A final archive is verified by decoding it and comparing
  its content with the hash-verified placeholder (`EqualIgnoringArchiveDigest`).
  Compression is never reproduced to verify received bytes.
- **Re-encoding stays in two places only**: legacy `VerifyArchiveDigest` for
  callers that have no placeholder (client fallback, handled in a follow-up
  change), and server-side `DerivePlaceholderArchive`, which keeps the derived
  bytes only when their hash equals the digest.
- **Placeholder blobs** live at `placeholder-archive-sha256-<hex>` in the blob
  store. Authorization is by archive digest through the existing revision
  lookup, so blob-owner indexing is unchanged.
- **Reuse** happens in the worker after build and before publication, through
  one function shared by the refresh and managed-refresh paths. It substitutes the
  stored archive digest and rebuilds the registry revision manifest digest.
  When the stored revision has no placeholder, the newly built placeholder is
  dropped rather than stored under a digest it does not hash to.
- **Backfill** runs at worker startup in the background and at the end of
  `serve reindex`, in bounded batches.

## Risks

- A final archive can differ in bytes from the encoder output while carrying the
  same verified content. This is accepted by design; content tampering is
  still rejected.
- A stored revision with no placeholder whose digest cannot be reproduced keeps
  the legacy artifact path, which re-encodes and can fail on a foreign toolchain
  until it is reindexed under the original toolchain.
