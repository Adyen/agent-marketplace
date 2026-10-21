<script setup lang="ts">
import { computed, onUnmounted, ref } from 'vue'

const props = withDefaults(
  defineProps<{ command: string; copyable?: boolean; highlight?: string }>(),
  { copyable: true, highlight: '' },
)

const commandParts = computed(() => {
  const start = props.highlight ? props.command.indexOf(props.highlight) : -1
  if (start < 0) return undefined

  return {
    before: props.command.slice(0, start),
    highlight: props.highlight,
    after: props.command.slice(start + props.highlight.length),
  }
})

type State = 'idle' | 'copied' | 'failed'
const COPIED_HOLD_MS = 1500

const state = ref<State>('idle')
let reset: ReturnType<typeof setTimeout> | undefined
let unmounted = false

const announcement = computed(
  () => ({ idle: '', copied: 'Command copied', failed: 'Copy failed' })[state.value],
)

async function copy(command: string) {
  let next: State = 'copied'
  try {
    await navigator.clipboard.writeText(command)
  } catch {
    next = 'failed'
  }
  if (unmounted) return
  clearTimeout(reset)
  state.value = next
  reset = setTimeout(() => (state.value = 'idle'), COPIED_HOLD_MS)
}

onUnmounted(() => {
  unmounted = true
  clearTimeout(reset)
})
</script>

<template>
  <div class="command-bar" :class="{ 'is-copyless': !copyable }">
    <span class="prompt" aria-hidden="true">$</span>
    <code>
      <template v-if="commandParts">
        {{ commandParts.before }}<span class="highlight">{{ commandParts.highlight }}</span>{{ commandParts.after }}
      </template>
      <template v-else>{{ command }}</template>
    </code>
    <button
      v-if="copyable"
      class="copy am-hit-target"
      type="button"
      :class="{ 'is-done': state === 'copied' }"
      :data-roll-active="state === 'copied'"
      aria-label="Copy command"
      @click="copy(command)"
    >
      <span class="am-roll-label copy__label">
        <span class="am-roll-face copy__face">{{ state === 'failed' ? '⌘C' : 'Copy' }}</span>
        <span class="am-roll-face am-roll-face-copy copy__face" aria-hidden="true">Copied</span>
      </span>
    </button>
    <span class="visually-hidden" role="status">{{ announcement }}</span>
  </div>
</template>

<style scoped>
.command-bar {
  display: flex;
  min-width: 0;
  height: var(--lp-h-control);
  align-items: center;
  gap: var(--lp-space-tight);
  padding: 0 var(--lp-space-related) 0 var(--lp-space-default);
  border-radius: var(--lp-radius-control);
  box-shadow: var(--lp-outline-shadow);
  font: var(--lp-font-label);
}

.command-bar.is-copyless {
  padding-right: var(--lp-space-default);
}

.prompt {
  color: var(--lp-text-2);
  user-select: none;
}

code {
  flex: 1;
  min-width: 0;
  padding-block: 0.25rem;
  margin-block: -0.25rem;
  overflow-x: auto;
  color: var(--lp-text);
  font: inherit;
  scrollbar-color: var(--lp-outline) transparent;
  scrollbar-width: thin;
  white-space: nowrap;
}

.highlight {
  color: var(--lp-code-flag);
}

.copy {
  position: relative;
  isolation: isolate;
  display: block;
  flex: none;
  height: var(--lp-h-depiction);
  padding: 0 var(--lp-space-related);
  border: 0;
  border-radius: var(--lp-radius-control);
  background: var(--lp-fill);
  color: var(--lp-text);
  font: inherit;
  text-transform: uppercase;
  cursor: pointer;
  transition: background-color var(--lp-motion-feedback) var(--lp-ease);
}

.copy::before {
  position: absolute;
  z-index: -1;
  inset: 0;
  border-radius: inherit;
  background: var(--lp-surface);
  content: "";
  opacity: 0;
  transition: opacity var(--lp-motion-feedback) var(--lp-ease);
}

.copy:hover::before {
  opacity: 1;
}

.copy:active::before {
  opacity: 0;
}

.copy.is-done {
  background: var(--lp-green);
  color: var(--lp-on-green);
}

/* Both labels share one grid cell so the control reserves the wider one. */
.copy__label {
  height: 100%;
  margin-inline: -0.125rem;
  padding-inline: 0.125rem;
}

.copy__face {
  justify-content: center;
}

@media (prefers-reduced-motion: reduce) {
  .copy,
  .copy::before {
    transition: none;
  }
}
</style>
