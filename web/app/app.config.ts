export default defineAppConfig({
  ui: {
    colors: {
      // Scales are defined in app/assets/css/main.css. Saturation is reserved
      // for verdicts, so the three status colours map to the three states a
      // revision can be in, and `primary` is an interaction-only blue that is
      // deliberately not any of them.
      primary: 'focus',
      neutral: 'ink',
      success: 'ready',
      warning: 'withheld',
      error: 'revoked',
    },
  },
})
