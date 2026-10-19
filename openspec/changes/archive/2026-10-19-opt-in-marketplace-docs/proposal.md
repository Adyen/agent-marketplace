## Why

Air-gapped Marketplace deployments need documentation served locally rather
than links to the public project website. The documentation-only artifact
already exists, but the Marketplace header always links to public docs and
the Compose examples deploy documentation unconditionally.

## What Changes

- Keep public documentation as the default for every Marketplace docs link.
- Reuse Helm's `docs.enabled`, defaulting to false, to select local docs.
- Add explicit Compose documentation overrides that enable the service,
  proxy route, and web-interface setting together.
- Keep local documentation at origin-root `/docs/`, independent of the
  Marketplace web-interface base path.

## Capabilities

### Modified Capabilities

- `site-deployment-profiles`: make Marketplace documentation hosting opt-in.
- `marketplace-web-ui`: select documentation destinations at runtime.

## Impact

Nuxt runtime configuration and documentation links, Helm frontend wiring,
both Compose deployments and their proxy profiles, deployment checks, and
operator documentation. This is not a global offline mode and does not
change registry acquisition, identity providers, or telemetry.
