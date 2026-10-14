## 1. Brand assets and presentation

- [x] 1.1 Add the supplied light, dark, and favicon files to both frontend public outputs and verify source bytes and image dimensions.
- [x] 1.2 Render light/dark logo variants and base-path-aware favicon links in Nuxt and VitePress while preserving the accessible product home link.
- [x] 1.3 Define shared width and optical-alignment tokens, apply them to both frontend logo images, and preserve route-dependent wrapper geometry.

## 2. Contract and verification

- [x] 2.1 Add the `// spec: marketplace-web-ui/browser-surfaces-present-accessible-adaptive-branding` E2E witness for assets, accessibility, base paths, appearance variants, visible image width, and optical alignment across Landing, Docs, and Marketplace.
- [x] 2.2 Run focused frontend checks, builds, responsive browser coverage, `make validate`, and `make spec-check`, then review deployment-path, accessibility, and narrow-navigation behavior.
- [x] 2.3 Run `openspec archive add-marketplace-brand-assets --yes` in the same pull request and verify the archived delta is synchronized into the main specification.
