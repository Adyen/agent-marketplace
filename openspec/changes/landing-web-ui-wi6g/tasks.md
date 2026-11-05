# Tasks

## 1. Hero preview tabs

- [x] 1.1 Add failing browser tests in `web/tests/e2e/landing.spec.ts` for the Terminal default, the Web caption switch, hero keyboard wrap, hero/harness independence, and reduced-motion switching (verify: the spec fails on the base commit for the intended reason)
- [x] 1.2 Add the hero Terminal/Web tab list reusing `LandingTabList` with its own `idPrefix` and selection, the caption switch, and the Web panel in `HeroSection.vue`; make 1.1 pass (verify: the Playwright landing spec passes)
- [x] 1.3 Add static HTML assertions in `site/tests/public-site.test.ts` for the hero tab IDs and tabpanel relationships (verify: `pnpm run test:public` in `site`)

## 2. Capture pipeline and fixtures

- [x] 2.1 Add realistic fixtures under `web/tests/e2e/` (several registries, skills, commands, MCP servers, a collection, one resource with a clean security assessment, no real third-party or Adyen-internal names) (verify: the fixtures load in `mock-marketplace.mjs` and the catalog renders them in a test)
- [x] 2.2 Add the Playwright capture script that runs the real Nuxt app against the fixtures and writes light and dark images of the catalog and the resource detail page at desktop and narrow widths, plus a manifest (verify: the script writes every manifest entry on a clean run)
- [x] 2.3 Add the reproducibility test that runs the capture twice and compares manifest and hashes (verify: the test passes and fails when a fixture changes)
- [x] 2.4 Add `make site-screenshots` and document the rerun policy in `site/README.md` (verify: the target runs and `site/README.md` names it)
- [ ] 2.5 Commit the generated images under `site/public/landing/` (WebP or, with the recorded reason, PNG) and record the total added payload in the PR notes (verify: the manifest matches the committed files)

## 3. For organizations browser frame

- [x] 3.1 Replace the `TerminalFrame` mock in `ForOrganizationsSection.vue` with a flat browser frame showing the catalog and resource detail variants with explicit dimensions, alt text, and lazy loading (verify: `pnpm run test:public` static assertions for dimensions, alt text, lazy loading, and the neutral host)
- [x] 3.2 Confirm the three benefit cards and the Deployment guide link remain, tightening copy only if a card is redundant (verify: the static HTML test asserts the cards and link)

## 4. Light and dark parity

- [x] 4.1 Add the dark/light depiction switch keyed on `html.dark` with appearance toggling and a browser test that toggles without reload (verify: the Playwright landing spec passes for the dark-class scenario)
- [x] 4.2 Confirm no page links to or prefetches a local `/marketplace/` route (verify: `pnpm run test:public` and `pnpm run test:docs`)

## 5. Spec and integration

- [x] 5.1 Bind every new scenario with a `// spec: marketplace-web-ui/<scenario-slug>` annotation in the test files and run `go run ./tools/specaudit trace` (verify: trace reports no unbound scenarios)
- [ ] 5.2 Run `openspec validate landing-web-ui-wi6g --strict` and `make spec-check` (verify: both pass)
- [x] 5.3 Run `make site-screenshots` twice on the unchanged tree and confirm no visible diff (verify: git status clean after the second run)
- [ ] 5.4 Run the final verification commands from the contract: `make spec-check && make site-screenshots && (cd site && pnpm run test:public && pnpm run test:docs)` (verify: all succeed on the final commit)
