<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { thenByChoices, type GroupBy, type ThenBy, type ViewSettings } from './grouping'
import GuiIcon from './GuiIcon.vue'

const props = defineProps<{ settings: ViewSettings }>()

const emit = defineEmits<{
  change: [settings: ViewSettings]
}>()

const groupByOptions: { value: GroupBy; label: string }[] = [
  { value: 'status', label: 'Status' },
  { value: 'source', label: 'Source' },
  { value: 'kind', label: 'Kind' },
]
const axisLabels: Record<ThenBy, string> = { none: 'None', kind: 'Kind', source: 'Source' }
const thenByOptions = computed(() => thenByChoices(props.settings.groupBy))

const open = ref(false)
const root = ref<HTMLElement | null>(null)
const button = ref<HTMLButtonElement | null>(null)

function openMenu() {
  if (open.value) {
    return
  }
  open.value = true
  void nextTick(() => root.value?.querySelector<HTMLInputElement>('input:checked')?.focus())
}

function closeMenu(refocus = true) {
  open.value = false
  if (refocus) {
    button.value?.focus()
  }
}

function toggleMenu() {
  if (open.value) {
    closeMenu()
  } else {
    openMenu()
  }
}

function chooseGroupBy(groupBy: GroupBy) {
  const thenBy = props.settings.thenBy === groupBy ? 'none' : props.settings.thenBy
  emit('change', { groupBy, thenBy })
}

function chooseThenBy(thenBy: ThenBy) {
  emit('change', { groupBy: props.settings.groupBy, thenBy })
}

function onPointerDown(event: PointerEvent) {
  if (open.value && event.target instanceof Node && root.value?.contains(event.target) !== true) {
    closeMenu(false)
  }
}

onMounted(() => window.addEventListener('pointerdown', onPointerDown))
onBeforeUnmount(() => window.removeEventListener('pointerdown', onPointerDown))

defineExpose({ open: openMenu })
</script>

<template>
  <div ref="root" class="gui-view-menu">
    <button
      ref="button"
      type="button"
      class="gui-icon-button"
      aria-label="View"
      aria-haspopup="true"
      :aria-expanded="open"
      :aria-controls="open ? 'workspace-view-panel' : undefined"
      @click="toggleMenu"
    ><GuiIcon name="view" /></button>
    <div
      v-if="open"
      id="workspace-view-panel"
      class="gui-view-panel"
      @keydown.esc.stop.prevent="closeMenu()"
    >
      <fieldset class="gui-view-panel__group">
        <legend class="gui-caps">Group by</legend>
        <label v-for="option in groupByOptions" :key="option.value" class="gui-view-panel__option">
          <input
            type="radio"
            name="view-group-by"
            :value="option.value"
            :checked="settings.groupBy === option.value"
            @change="chooseGroupBy(option.value)"
          />
          <span>{{ option.label }}</span>
        </label>
      </fieldset>
      <fieldset class="gui-view-panel__group">
        <legend class="gui-caps">Then by</legend>
        <label v-for="axis in thenByOptions" :key="axis" class="gui-view-panel__option">
          <input
            type="radio"
            name="view-then-by"
            :value="axis"
            :checked="settings.thenBy === axis"
            @change="chooseThenBy(axis)"
          />
          <span>{{ axisLabels[axis] }}</span>
        </label>
      </fieldset>
    </div>
  </div>
</template>
