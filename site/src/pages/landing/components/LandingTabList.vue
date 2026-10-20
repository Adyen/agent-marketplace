<script setup lang="ts">
import { nextTick, ref } from 'vue'

import { nextTabIndex } from '../composables/tabNavigation'
import { useTabIndicator } from '../composables/useTabIndicator'

type Tab = {
  id: string
  label: string
}

const props = defineProps<{
  idPrefix: string
  label: string
  modelValue: string
  tabs: readonly Tab[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const root = ref<HTMLElement | null>(null)
const { placement, animated } = useTabIndicator(root, () => props.modelValue)

function tabID(id: string) {
  return `${props.idPrefix}-tab-${id}`
}

function panelID(id: string) {
  return `${props.idPrefix}-panel-${id}`
}

async function select(index: number) {
  const tab = props.tabs[index]
  if (!tab) return

  emit('update:modelValue', tab.id)
  await nextTick()
  root.value?.querySelector<HTMLElement>(`#${tabID(tab.id)}`)?.focus()
}

function navigate(event: KeyboardEvent, current: number) {
  const next = nextTabIndex(event.key, current, props.tabs.length)
  if (next === null) return

  event.preventDefault()
  void select(next)
}
</script>

<template>
  <div
    ref="root"
    class="landing-tabs"
    :class="{ 'has-mark': placement }"
    role="tablist"
    :aria-label="label"
  >
    <span
      v-if="placement"
      class="tab-mark"
      :class="{ 'is-animated': animated }"
      :style="placement"
      aria-hidden="true"
    />
    <button
      v-for="(tab, index) in tabs"
      :id="tabID(tab.id)"
      :key="tab.id"
      class="tab am-hit-target"
      type="button"
      role="tab"
      :aria-controls="panelID(tab.id)"
      :aria-selected="tab.id === modelValue"
      :tabindex="tab.id === modelValue ? 0 : -1"
      @click="emit('update:modelValue', tab.id)"
      @keydown="navigate($event, index)"
    >
      {{ tab.label }}
    </button>
  </div>
</template>

<style scoped>
.landing-tabs {
  position: relative;
  display: flex;
  flex-wrap: wrap;
  gap: var(--lp-space-tight);
}

.tab {
  position: relative;
  z-index: 1;
  display: block;
  min-height: var(--lp-h-compact);
  padding: 0 var(--lp-space-related);
  border: 0;
  border-radius: var(--lp-radius-control);
  background: none;
  color: var(--lp-text-2);
  font: var(--lp-font-label);
  text-transform: uppercase;
  white-space: nowrap;
  cursor: pointer;
  transition: color var(--lp-motion-feedback) var(--lp-ease);
}

.tab:hover,
.tab[aria-selected="true"] {
  color: var(--lp-text);
}

.landing-tabs:not(.has-mark) .tab[aria-selected="true"] {
  background: var(--lp-fill);
}

.tab-mark {
  position: absolute;
  top: 0;
  left: 0;
  z-index: 0;
  height: var(--lp-h-compact);
  border-radius: var(--lp-radius-control);
  background: var(--lp-fill);
  pointer-events: none;
}

.tab-mark.is-animated {
  transition:
    transform var(--lp-motion-feedback) var(--lp-ease),
    width var(--lp-motion-feedback) var(--lp-ease);
}

@media (max-width: 959px) {
  .landing-tabs {
    row-gap: var(--lp-space-related);
  }

}

@media (prefers-reduced-motion: reduce) {
  .tab,
  .tab-mark.is-animated {
    transition: none;
  }
}
</style>
