<script setup lang="ts">
import { ref } from 'vue'

withDefaults(
  defineProps<{
    label: string
    status?: string
    tag?: 'div' | 'figure'
    padded?: boolean
  }>(),
  {
    tag: 'div',
    padded: false,
  },
)

const frame = ref<HTMLElement | null>(null)

defineExpose({
  focus: () => frame.value?.focus(),
})
</script>

<template>
  <component
    :is="tag"
    ref="frame"
    class="terminal-frame"
    :class="{ 'is-padded': padded }"
  >
    <div class="terminal-header">
      <span class="window-controls" aria-hidden="true">
        <i></i><i></i><i></i>
      </span>
      <span class="terminal-label">{{ label }}</span>
      <span v-if="status || $slots.status" class="terminal-status">
        <slot name="status">{{ status }}</slot>
      </span>
    </div>
    <div class="terminal-body">
      <slot />
    </div>
  </component>
</template>

<style scoped>
.terminal-frame {
  min-width: 0;
  margin: 0;
  overflow: hidden;
  border: 1px solid var(--am-terminal-border);
  border-radius: var(--am-radius-lg);
  background: var(--am-terminal-bg);
  box-shadow: var(
    --am-terminal-shadow,
    0 34px 72px -38px var(--am-code-shadow),
    0 0 0 1px var(--am-accent-border),
    0 0 52px var(--am-accent-soft)
  );
  color: var(--am-terminal-text);
  font-family: var(--vp-font-family-mono);
  font-size: var(--am-terminal-font-size, 14px);
  line-height: var(--am-terminal-line-height, 1.5);
}

.terminal-header {
  display: grid;
  grid-template-columns: 1fr auto 1fr;
  align-items: center;
  gap: 12px;
  padding: 11px 18px;
  border-bottom: 1px solid var(--am-terminal-border);
  background: var(--am-terminal-bg-raised);
  color: var(--am-terminal-muted);
  font-size: 12px;
}

.window-controls {
  grid-column: 1;
  display: inline-flex;
  gap: 6px;
  justify-self: start;
}

.window-controls i {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #67746c;
}

.window-controls i:nth-child(2) {
  background: #8b7850;
}

.window-controls i:nth-child(3) {
  background: var(--am-terminal-accent);
}

.terminal-label {
  grid-column: 2;
  white-space: nowrap;
}

.terminal-status {
  grid-column: 3;
  justify-self: end;
  color: var(--am-terminal-status-color, var(--am-terminal-warning));
  white-space: nowrap;
}

.is-padded .terminal-body {
  padding: 22px;
}

@media (max-width: 680px) {
  .terminal-header {
    grid-template-columns: auto 1fr auto;
  }

  .is-padded .terminal-body {
    padding: 16px;
  }
}
</style>
