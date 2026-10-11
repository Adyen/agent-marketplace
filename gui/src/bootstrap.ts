export interface GuiBootstrap {
  url: string
  credential: string
  version: string
}

interface WailsBootstrap {
  credential: string
  version: string
}

declare global {
  interface Window {
    __AM_GUI_BOOTSTRAP__?: { url?: string; credential: string; version?: string }
    go?: { main?: { App?: { Bootstrap?: () => Promise<WailsBootstrap>; Quit?: () => Promise<void> } } }
    runtime?: { EventsOn?: (event: string, callback: (...data: unknown[]) => void) => (() => void) | void }
  }
}

export const launchRequestEvent = 'desktop:launch-request'

export const closeRequestEvent = 'desktop:close-request'

export function nativeQuit(): (() => void) | undefined {
  const quit = window.go?.main?.App?.Quit
  if (quit == null) {
    return undefined
  }
  return () => {
    void quit()
  }
}

export async function resolveBootstrap(): Promise<GuiBootstrap> {
  const bridge = window.go?.main?.App?.Bootstrap
  if (bridge) {
    const bound = await bridge()
    return { url: '', credential: bound.credential, version: bound.version }
  }
  const injected = window.__AM_GUI_BOOTSTRAP__
  if (injected?.credential) {
    return {
      url: injected.url ?? window.location.origin,
      credential: injected.credential,
      version: injected.version ?? 'dev',
    }
  }
  throw new Error('no trusted bootstrap available')
}
