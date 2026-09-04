<script setup lang="ts">
defineProps<{ label?: string }>()
</script>

<template>
  <div class="code-block">
    <div v-if="label" class="label">{{ label }}</div>
    <div class="content"><slot /></div>
  </div>
</template>

<style scoped>
.code-block {
  overflow-x: auto;
  border: 1px solid var(--am-code-border);
  border-radius: var(--am-radius-lg);
  background: var(--am-code-bg);
  box-shadow:
    0 24px 50px -34px var(--am-code-shadow),
    inset 0 1px rgba(255, 255, 255, 0.025);
  color: var(--am-code-text);
  font-family: var(--vp-font-family-mono);
  font-size: var(--am-technical-font-size);
  line-height: var(--am-technical-line-height);
}

.label {
  padding: 10px var(--am-technical-padding) 9px;
  border-bottom: 1px solid var(--am-code-border);
  color: var(--am-code-muted);
  font-size: 12px;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.content { padding: var(--am-technical-padding); }
</style>
