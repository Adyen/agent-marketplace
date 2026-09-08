<script setup lang="ts">
const { isDark, toggleAppearance } = useSharedAppearance()
const appearanceLabel = computed(() =>
  isDark.value ? 'Switch to light theme' : 'Switch to dark theme'
)
</script>

<template>
  <button
    type="button"
    role="switch"
    class="am-theme-switch"
    :aria-checked="isDark"
    :aria-label="appearanceLabel"
    :title="appearanceLabel"
    @click="toggleAppearance"
  >
    <span class="am-theme-switch__thumb" aria-hidden="true">
      <svg class="am-theme-switch__icon am-theme-switch__icon--sun" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2">
        <circle cx="12" cy="12" r="4" />
        <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41" />
      </svg>
      <svg class="am-theme-switch__icon am-theme-switch__icon--moon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2">
        <path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z" />
      </svg>
    </span>
  </button>
</template>
