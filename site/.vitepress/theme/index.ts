import type { Theme } from 'vitepress'
import DefaultTheme from 'vitepress/theme'

import LandingPage from '../../src/pages/landing/LandingPage.vue'

import './styles/tokens.css'
import './styles/vitepress.css'

export default {
  extends: DefaultTheme,

  enhanceApp({ app }) {
    app.component('LandingPage', LandingPage)
  }
} satisfies Theme
