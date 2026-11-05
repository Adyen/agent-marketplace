## Why

Decoding a resource artifact correctly rejects an archive entry whose name is absolute or contains `..` path components, but the returned error quotes that rejected name verbatim. Through error wrapping, untrusted archive text that just failed path validation reaches CLI output, logs, and downstream errors. GitHub CodeQL reports this as an archive-path flow into filesystem-consuming code.

## What Changes

- Report absolute-path and path-traversal decode rejections without the rejected entry name.
- Keep every existing rejection, its reason, and the decoding of valid resource artifacts unchanged.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-api`: decode rejections omit the unsafe archive entry name.

## Impact

Error text for resource artifacts rejected during decoding, on the client and the Marketplace. Valid archives, digests, and wire formats are unchanged.
