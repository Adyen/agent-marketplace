import { addNonceToScripts } from '../utils/security-policy'

export default defineNitroPlugin(nitroApp => {
  nitroApp.hooks.hook('render:html', (html, { event }) => {
    const nonce = event.context.cspNonce
    if (typeof nonce !== 'string') return

    for (const section of ['head', 'bodyPrepend', 'body', 'bodyAppend'] as const) {
      html[section] = html[section].map(element => addNonceToScripts(element, nonce))
    }
  })
})
