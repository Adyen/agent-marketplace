<script setup lang="ts">
import { computed, onUnmounted, ref } from "vue";

const props = withDefaults(
  defineProps<{ command: string; copyable?: boolean; highlight?: string }>(),
  { copyable: true, highlight: "" },
);

const commandParts = computed(() => {
  const start = props.highlight ? props.command.indexOf(props.highlight) : -1;
  if (start < 0) return undefined;

  return {
    before: props.command.slice(0, start),
    highlight: props.highlight,
    after: props.command.slice(start + props.highlight.length),
  };
});

type State = "idle" | "copied" | "failed";
const state = ref<State>("idle");
let reset: ReturnType<typeof setTimeout> | undefined;

const labels: Record<State, string> = {
  idle: "copy",
  copied: "copied",
  failed: "⌘C",
};

async function copy(command: string) {
  try {
    await navigator.clipboard.writeText(command);
    state.value = "copied";
  } catch {
    state.value = "failed";
  }
  clearTimeout(reset);
  reset = setTimeout(() => (state.value = "idle"), 1400);
}

onUnmounted(() => clearTimeout(reset));
</script>

<template>
  <div class="command-bar">
    <span class="prompt">$</span>
    <code>
      <template v-if="commandParts">
        {{ commandParts.before
        }}<span class="highlight">{{ commandParts.highlight }}</span
        >{{ commandParts.after }}
      </template>
      <template v-else>{{ command }}</template>
    </code>
    <button
      v-if="copyable"
      class="copy"
      :class="{ 'is-done': state === 'copied' }"
      aria-label="Copy command"
      @click="copy(command)"
    >
      {{ labels[state] }}
    </button>
  </div>
</template>

<style scoped>
.command-bar {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 11px;
  padding: 12px var(--am-technical-padding);
  border: 1px solid var(--am-code-border);
  border-radius: var(--am-radius-lg);
  background: var(--am-code-bg);
  box-shadow: 0 12px 30px -25px var(--am-code-shadow);
  font-family: var(--vp-font-family-mono);
  font-size: var(--am-technical-font-size);
  line-height: var(--am-technical-line-height);
}

code {
  min-width: 0;
  overflow-x: auto;
  color: var(--am-code-text);
  font: inherit;
  scrollbar-color: var(--am-code-border) transparent;
  scrollbar-width: thin;
  white-space: nowrap;
}

code::-webkit-scrollbar {
  height: 4px;
}

code::-webkit-scrollbar-thumb {
  border-radius: 999px;
  background: var(--am-code-border);
}

.highlight {
  color: var(--vp-c-brand-1);
}
.prompt {
  color: var(--vp-c-brand-1);
  user-select: none;
}
.copy {
  padding: 6px 9px;
  border: 1px solid var(--vp-c-brand-1);
  border-radius: var(--am-radius-sm);
  background: var(--vp-c-brand-1);
  color: var(--am-on-accent);
  font-size: 12px;
  font-weight: 600;
  line-height: 1;
  cursor: pointer;
}
.copy:hover,
.copy.is-done {
  border-color: var(--vp-c-brand-2);
  background: var(--vp-c-brand-2);
  color: var(--am-on-accent);
}
.copy:active {
  transform: translateY(1px);
}
.copy:focus-visible {
  outline: 2px solid var(--vp-c-brand-1);
  outline-offset: 2px;
}
</style>
