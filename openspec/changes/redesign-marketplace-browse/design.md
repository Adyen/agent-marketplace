## Context

The browse route is the Marketplace's primary index, but its content width,
row typography, filter typography, and visual boundaries do not yet use the
shared browser foundations.

## Decisions

Use the Landing/Docs content equation for navigation, introduction, catalog,
and footer: `min(100% - 2 * gutter, 1440px)`.

Keep the catalog dense and flat. The introduction is an ordinary section title
with one line of supporting copy, controls remain sticky on wide screens, and
filters disclose on narrow screens.

Use Adyen Sans for the entire browse interface. Keep Kind, registry, slash, and
resource name on their existing top line at 16/24; regular muted context and a
semibold primary name provide hierarchy without moving content. Reserve Mono
for actual commands and code blocks.

Keep the installation count in the existing utility column, reserve equal
utility slots so rows remain equal height, and preserve all interaction and
availability behavior.
