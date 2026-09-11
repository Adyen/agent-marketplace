import { SHARED_APPEARANCE_KEY } from '~/lib/site'

type AppearancePreference = 'auto' | 'dark' | 'light'

function storedPreference(): AppearancePreference {
  if (!import.meta.client) return 'auto'

  try {
    const value = window.localStorage.getItem(SHARED_APPEARANCE_KEY)
    return value === 'dark' || value === 'light' ? value : 'auto'
  }
  catch {
    return 'auto'
  }
}

function prefersDark() {
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? false
}

export function useSharedAppearance() {
  const isDark = useState('shared-appearance-dark', () => false)
  let mediaQuery: MediaQueryList | undefined

  function applyPreference(preference: AppearancePreference, persist = false) {
    if (!import.meta.client) return

    const nextIsDark = preference === 'auto' ? prefersDark() : preference === 'dark'
    document.documentElement.classList.toggle('dark', nextIsDark)
    isDark.value = nextIsDark

    if (!persist) return
    try {
      window.localStorage.setItem(SHARED_APPEARANCE_KEY, preference)
    }
    catch {
      // The class still changes when browser storage is unavailable.
    }
  }

  function toggleAppearance() {
    applyPreference(isDark.value ? 'light' : 'dark', true)
  }

  function syncStoredAppearance(event?: StorageEvent) {
    if (event && event.key !== SHARED_APPEARANCE_KEY) return
    applyPreference(storedPreference())
  }

  function syncSystemAppearance() {
    if (storedPreference() === 'auto') applyPreference('auto')
  }

  onMounted(() => {
    isDark.value = document.documentElement.classList.contains('dark')
    mediaQuery = window.matchMedia?.('(prefers-color-scheme: dark)')
    mediaQuery?.addEventListener('change', syncSystemAppearance)
    window.addEventListener('storage', syncStoredAppearance)
  })

  onBeforeUnmount(() => {
    mediaQuery?.removeEventListener('change', syncSystemAppearance)
    window.removeEventListener('storage', syncStoredAppearance)
  })

  return {
    isDark: readonly(isDark),
    toggleAppearance,
  }
}
