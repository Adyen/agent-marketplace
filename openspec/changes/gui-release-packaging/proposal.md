## Why

This open-source repository cannot hold organization signing identities or secrets. It should publish reproducible unsigned desktop artifacts that an organization's downstream distributor can sign and notarize without rebuilding, while leaving the statically linked CLI artifacts unchanged.

## What Changes

- Publish an unsigned `.dmg` per supported macOS architecture and an unsigned `.rpm` per supported Linux architecture, with checksums alongside them.
- Build the macOS `.app` for the hardened runtime with its notarization entitlements and register the `agent-marketplace` URL scheme; declare WebKitGTK and register the scheme in the RPM desktop entry.
- Prove downstream signability in every pull-request and tagged-release build with ad-hoc macOS signing and a throwaway RPM signing key.
- Keep organization identities and signing secrets out of the repository and its workflows; keep pull-request artifacts unsigned and smoke-tested.
- Keep CLI release artifacts statically linked and unchanged.

## Capabilities

### New Capabilities
- `gui-distribution`: Unsigned GUI release packages, downstream signability, CI validation, checksums, URL-scheme registration, and unchanged CLI release artifacts.

### Modified Capabilities

## Impact

Release packaging and CI validation for macOS/Linux GUI artifacts are affected. Organizations sign, notarize, and redistribute artifacts downstream using identities held outside this public repository. Unsupported package formats and operating systems remain out of scope.
