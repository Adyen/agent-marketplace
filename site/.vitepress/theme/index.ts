import type { Theme } from "vitepress";
import DefaultTheme from "vitepress/theme-without-fonts";

import LandingPage from "../../src/pages/landing/LandingPage.vue";
import MarketplaceNavLink from "../../src/shared/components/MarketplaceNavLink.vue";
import SiteLayout from "../../src/shared/components/SiteLayout.vue";

import "./styles/design-tokens.css";

export default {
  extends: DefaultTheme,
  Layout: SiteLayout,

  enhanceApp({ app }) {
    app.component("LandingPage", LandingPage);
    app.component("MarketplaceNavLink", MarketplaceNavLink);
  },
} satisfies Theme;
