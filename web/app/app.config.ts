export default defineAppConfig({
  ui: {
    colors: {
      // Scales are defined by the Nuxt adapter in design-tokens.css. Status
      // colors map to Marketplace outcomes; primary maps to the shared action
      // color and is never used as an availability claim.
      primary: 'focus',
      neutral: 'ink',
      success: 'ready',
      warning: 'withheld',
      error: 'revoked',
    },
  },
})
