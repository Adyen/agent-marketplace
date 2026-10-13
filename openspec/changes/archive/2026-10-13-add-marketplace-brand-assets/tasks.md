## 1. Brand assets

- [x] 1.1 Add the supplied light, dark, and favicon files to both frontend public outputs and verify the source bytes and image dimensions match the archive.
- [x] 1.2 Render the adaptive logo and base-path-aware favicon in Nuxt and VitePress while preserving the accessible product home link.

## 2. Contract and verification

- [x] 2.1 Add the `// spec: marketplace-web-ui/browser-surfaces-present-accessible-adaptive-branding` E2E witness and verify both shared-origin surfaces use the expected logo and favicon paths in light and dark appearance.
- [x] 2.2 Run focused frontend checks, builds, `make validate`, and `make spec-check`, then review the final diff for deployment-path and accessibility regressions.
- [x] 2.3 Run `openspec archive add-marketplace-brand-assets --yes` in the same pull request and verify the archived delta is synchronized into the main specification.
