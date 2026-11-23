<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'

defineProps<{ pendingCount: number | null }>()

const keyboardUsed = ref(false)
const navigationKeys = new Set(['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight', 'Enter', 'Tab'])

function onKeydown(event: KeyboardEvent) {
  if (navigationKeys.has(event.key)) {
    keyboardUsed.value = true
  }
}

onMounted(() => window.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <footer class="gui-statusbar">
    <span class="gui-statusbar__status mono">
      <span v-if="pendingCount !== null && pendingCount > 0"><span class="gui-dot gui-dot--warning" aria-hidden="true" /> {{ pendingCount }} pending sync</span>
      <span v-else-if="pendingCount === 0" class="gui-statusbar__synced"><span class="gui-statusbar__glyph">✓</span> in sync</span>
    </span>
    <span v-if="keyboardUsed" class="gui-statusbar__hints mono">
      <kbd class="gui-kbd">↑↓</kbd> move · <kbd class="gui-kbd">⏎</kbd> open · <kbd class="gui-kbd">s</kbd> sync
    </span>
  </footer>
</template>
