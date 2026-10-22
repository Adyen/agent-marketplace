## Context

`site/.vitepress/config.mts` builds two profiles from one source tree: `public` (landing and docs at `/`) and `docs` (documentation-only, shipped in self-hosted containers). Head tags are shared chrome, so a beacon added to `head` reaches every generated page of the public profile.

## Decisions

- **Token from the build environment, not source.** `SITE_CLOUDFLARE_ANALYTICS_TOKEN` is read in `config.mts`. Only the Pages publishing step passes it, through `vars.CLOUDFLARE_WEB_ANALYTICS_TOKEN` gated on `steps.pages.outcome == 'success'`, so PR builds and forks never receive it.
- **Profile gate in code.** `analyticsHead(profile, token)` returns no tags for the `docs` profile, even when the variable is set. The docs profile's absence is therefore enforced by the function and checked by `docs-profile.test.ts` against the generated HTML.
- **Strict token validation.** The token must match `^[0-9a-f]{32}$`. Validation runs before the value is serialized into the `data-cf-beacon` JSON attribute, so no markup can be injected through the variable.
- **Pure module.** `analyticsHead` lives in `site/src/shared/analytics.ts` so `node --test` can exercise it without building the site.

## Risks

- The beacon is a third-party script on the public site. It sets no cookies and needs no consent, which is the reason it was chosen over alternatives.
