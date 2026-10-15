<script setup lang="ts">
defineProps<{ pendingCount: number | null }>()
</script>

<template>
  <footer class="gui-statusbar">
    <span class="gui-statusbar__status mono">
      <span v-if="pendingCount !== null && pendingCount > 0">~ {{ pendingCount }} pending sync</span>
      <span v-else-if="pendingCount === 0" class="gui-statusbar__synced">✓ in sync</span>
    </span>
    <span class="gui-statusbar__hints mono">
      <kbd class="gui-kbd">↑↓</kbd> move · <kbd class="gui-kbd">space</kbd> select · <kbd class="gui-kbd">⏎</kbd> open · <kbd class="gui-kbd">s</kbd> sync
    </span>
  </footer>
</template>
