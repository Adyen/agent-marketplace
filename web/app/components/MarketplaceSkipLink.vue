<script setup lang="ts">
defineProps<{
  target: string
}>()
</script>

<template>
  <a :href="`#${target}`" class="am-skip-link">Skip to content</a>
</template>
