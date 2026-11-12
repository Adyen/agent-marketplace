import { ref, readonly } from 'vue'

export type AppearancePreference = 'system' | 'light' | 'dark'

export const appearanceStorageKey = 'vitepress-theme-appearance'

const darkQuery = typeof window !== 'undefined' ? window.matchMedia?.('(prefers-color-scheme: dark)') : undefined

function storedPreference(): AppearancePreference {
  try {
    const value = window.localStorage.getItem(appearanceStorageKey)
    return value === 'dark' || value === 'light' ? value : 'system'
  } catch {
    return 'system'
  }
}

function applyTheme(preference: AppearancePreference) {
  const dark = preference === 'dark' || (preference === 'system' && (darkQuery?.matches ?? false))
  document.documentElement.classList.toggle('dark', dark)
}

const preference = ref<AppearancePreference>(storedPreference())

darkQuery?.addEventListener('change', () => {
  if (preference.value === 'system') {
    applyTheme('system')
  }
})

export function useAppearance() {
  function setPreference(next: AppearancePreference) {
    preference.value = next
    try {
      if (next === 'system') {
        window.localStorage.removeItem(appearanceStorageKey)
      } else {
        window.localStorage.setItem(appearanceStorageKey, next)
      }
    } catch {
      // The class still changes when browser storage is unavailable.
    }
    applyTheme(next)
  }

  return { preference: readonly(preference), setPreference }
}
