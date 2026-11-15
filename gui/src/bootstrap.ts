import type { EventBridge } from './api/client'

export interface GuiBootstrap {
  url: string
  credential: string
  packageFormat: string
  version: string
}

interface WailsBootstrap {
  credential: string
  packageFormat: string
  version: string
}

declare global {
  interface Window {
    __AM_GUI_BOOTSTRAP__?: { url?: string; credential: string; packageFormat?: string; version?: string }
    go?: { main?: { App?: { Bootstrap?: () => Promise<WailsBootstrap>; Quit?: () => Promise<void> } } }
    runtime?: {
      EventsOn?: (event: string, callback: (...data: unknown[]) => void) => (() => void) | void
      EventsEmit?: (event: string, ...data: unknown[]) => void
      ClipboardSetText?: (text: string) => Promise<boolean>
    }
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

export function nativeEventBridge(): EventBridge | undefined {
  const eventsOn = window.runtime?.EventsOn
  const eventsEmit = window.runtime?.EventsEmit
  if (!eventsOn || !eventsEmit) {
    return undefined
  }
  return {
    on(event, callback) {
      const detach = eventsOn(event, callback)
      return typeof detach === 'function' ? detach : () => {}
    },
    emit(event, ...data) {
      eventsEmit(event, ...data)
    },
  }
}

export async function resolveBootstrap(): Promise<GuiBootstrap> {
  const bridge = window.go?.main?.App?.Bootstrap
  if (bridge) {
    const bound = await bridge()
    return { url: '', credential: bound.credential, packageFormat: bound.packageFormat, version: bound.version }
  }
  const injected = window.__AM_GUI_BOOTSTRAP__
  if (injected?.credential) {
    return {
      url: injected.url ?? window.location.origin,
      credential: injected.credential,
      packageFormat: injected.packageFormat ?? '',
      version: injected.version ?? 'dev',
    }
  }
  throw new Error('no trusted bootstrap available')
}
