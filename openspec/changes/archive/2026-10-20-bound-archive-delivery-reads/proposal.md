## Why

R21 (finding A76/F55) found public payload routes fetching whole blobs before applying existing byte ceilings. Content, inspection and manifest reads were bounded earlier; the remaining part was archive delivery. The artifact route and the placeholder archive route fetched final and placeholder-form archives in full before comparing them with the configured artifact limit, and storing a derived placeholder first read any existing blob at that key in full.

## What Changes

- The artifact route, the placeholder archive route (stored or derived), and final-archive verification against a stored placeholder read archive blobs through the existing bounded-read capability at the configured artifact limit. An oversized archive is refused with the existing `413` response before it is materialized.
- Storing an immutable blob compares an existing blob by reading at most one byte more than the upload; a longer existing blob is a conflict, as before.
- No limit is added or changed. Custom blob stores without the bounded capability keep their compatibility path and the existing post-read size check. Archive decompression is bounded by the verified-archive decoding requirement.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-api`: stored payload reads enforce existing byte ceilings for archive delivery too.

## Impact

`pkg/marketplace/api` archive routes and `pkg/marketplace/store` immutable blob writes. Responses are unchanged; only how much of an oversized blob is read changes.
