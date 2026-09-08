<script setup lang="ts">
defineProps<{
  target: string
}>()
</script>

<template>
  <a :href="`#${target}`" class="sr-only z-50 rounded-lg bg-primary px-4 py-3 text-inverted focus:not-sr-only focus:fixed focus:left-4 focus:top-4">Skip to content</a>
</template>
