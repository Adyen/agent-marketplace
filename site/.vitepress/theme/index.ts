import type { Theme } from 'vitepress'
import DefaultTheme from 'vitepress/theme'

import LandingPage from '../../src/pages/landing/LandingPage.vue'

import './styles/design-tokens.css'

export default {
  extends: DefaultTheme,

  enhanceApp({ app }) {
    app.component('LandingPage', LandingPage)
  }
} satisfies Theme
