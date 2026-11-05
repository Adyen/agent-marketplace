import type { Theme } from 'vitepress'
import DefaultTheme from 'vitepress/theme-without-fonts'

import LandingPage from '../../src/pages/landing/LandingPage.vue'
import SiteLayout from '../../src/shared/components/SiteLayout.vue'
import SiteNavLink from '../../src/shared/components/SiteNavLink.vue'

import './styles/design-tokens.css'
import './styles/landing-tokens.css'
import './styles/site-primitives.css'
import './styles/site-interactions.css'
import './styles/site-shell.css'

export default {
  extends: DefaultTheme,
  Layout: SiteLayout,

  enhanceApp({ app }) {
    app.component('LandingPage', LandingPage)
    app.component('SiteNavLink', SiteNavLink)
  }
} satisfies Theme
