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
      Browse a registry, choose resources for the right scope, then write native
      files for every configured harness.
    </template>

    <p class="try-instruction">
      <strong>Try it:</strong>
      Click the terminal to focus, then use <kbd>↑</kbd><kbd>↓</kbd> to navigate,
      <kbd>Space</kbd> to toggle, and <kbd>S</kbd> to sync.
    </p>

    <InteractiveTuiDemo class="demo" />
  </SectionShell>
</template>

<style scoped>
.demo {
  margin-top: 16px;
}

.try-instruction {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 36px;
  color: var(--vp-c-text-1);
  font-size: 15px;
}

.try-instruction strong {
  margin-right: 2px;
  color: var(--vp-c-brand-1);
}

kbd {
  padding: 2px 6px;
  border: 1px solid var(--am-accent-border);
  border-radius: var(--am-radius-sm);
  background: var(--am-accent-soft);
  color: var(--vp-c-text-1);
  font-family: var(--vp-font-family-mono);
  font-size: 12.5px;
  line-height: 1.2;
}
</style>
