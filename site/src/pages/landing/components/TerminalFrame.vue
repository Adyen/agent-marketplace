<script setup lang="ts">
import { ref } from 'vue'

import './terminal-screen.css'

withDefaults(
  defineProps<{
    label: string
    status?: string
    tag?: 'div' | 'figure'
  }>(),
  {
    tag: 'div',
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
