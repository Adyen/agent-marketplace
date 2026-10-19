<script setup lang="ts">
import InteractiveTuiDemo from '../components/InteractiveTuiDemo.vue'
import SectionShell from '../components/SectionShell.vue'
</script>

<template>
  <SectionShell
    id="flow"
    eyebrow="How it works"
    title="Select what you need. Sync it where you work"
    soft
  >
    <template #sub>
      Bring resources from marketplaces, Git registries, and local folders into one config.
      Select what you need and sync it to your AI tools.
    </template>

    <InteractiveTuiDemo />
  </SectionShell>
</template>
