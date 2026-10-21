## Why

Landing and Docs are one public-site product, but they currently use system
typography and an older green-tinted visual language. The public site needs one
approved foundation built from the Agent Marketplace palette, Adyen variable
fonts, shared spacing, and consistent navigation and footer chrome.

## What Changes

- Load the approved Adyen Sans and Mono variable fonts in the public site.
- Define Agent Marketplace Green `#00d16a`, Black `#121a18`, and white as the
  public-site palette, with shared light and dark semantic roles.
- Apply the same typography and color foundations to Landing and Docs.
- Restyle their shared VitePress navigation and footer without changing routes,
  destinations, article content, or Marketplace application presentation.

## Capabilities

### Modified Capabilities

- `marketplace-web-ui`: The shared browser identity now includes the public
  site's approved fonts, palette, and common Landing/Docs chrome.

## Impact

The change affects shared brand CSS, the VitePress theme, font assets, and
browser presentation tests. It does not change public routes, documentation
content, Marketplace application styling, or consumer behavior.
