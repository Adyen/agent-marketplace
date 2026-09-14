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
    class="am-theme-switch cursor-pointer"
    :aria-checked="isDark"
    :aria-label="appearanceLabel"
    :title="appearanceLabel"
    @click="toggleAppearance"
  >
    <UIcon :name="isDark ? 'i-lucide-moon' : 'i-lucide-sun'" class="am-theme-switch__icon" aria-hidden="true" />
  </button>
</template>
