<script lang="ts">
export function rowKey(id: string): string {
  return `row:${id}`
}

export function sectionKey(id: string): string {
  return `section:${id}`
}

export function rowElementId(id: string): string {
  return `row-${id}`
}

export function sectionElementId(id: string): string {
  return `section-${id}`
}
</script>

<script setup lang="ts">
import { computed } from 'vue'
import type { Row } from './controller'
import type { Section, ViewSettings } from './grouping'
import type { NameSegment } from './search'
import GuiIcon from './GuiIcon.vue'

export type ResourceSection = Omit<Section, 'children'> & { collapsed: boolean; children: ResourceSection[] }

const props = defineProps<{
  sections: ResourceSection[]
  selectedKey: string | null
  nameParts: (row: Row) => NameSegment[]
  viewSettings: ViewSettings
}>()

const emit = defineEmits<{
  toggleGroup: [path: string]
  select: [row: Row]
  activate: [row: Row]
  move: [direction: 1 | -1]
}>()

const entries = computed(() => props.sections.flatMap((section) => (section.collapsed ? [section] : [section, ...section.children])))
const hideKind = computed(() => props.viewSettings.groupBy === 'kind' || props.viewSettings.thenBy === 'kind')
const hideRegistry = computed(() => props.viewSettings.groupBy === 'source' || props.viewSettings.thenBy === 'source')
const rowColumns = computed(() =>
  ['3ch', ...(hideKind.value ? [] : ['10ch']), 'minmax(0, 1fr)', ...(hideRegistry.value ? [] : ['minmax(0, 24ch)'])].join(' '),
)

function scopeIsTag(scope: string): boolean {
  return scope === 'organization' || scope === 'project' || scope === 'local'
}

function problemReason(row: Row): { text: string; tone: 'danger' | 'warning' } | null {
  const tone = row.group === 'broken' ? 'danger' : row.group === 'malformed' || row.group === 'stale' ? 'warning' : null
  if (tone == null) {
    return null
  }
  const separator = row.status.indexOf(': ')
  return { text: separator === -1 ? row.status : row.status.slice(separator + 2), tone }
}
</script>

<template>
  <section aria-label="Resources" class="workspace__list gui-list">
    <div
      v-for="group in entries"
      :key="group.path"
      class="workspace__group"
      :class="{ 'workspace__group--nested': group.depth === 1 }"
    >
      <component :is="group.depth === 0 ? 'h2' : 'h3'">
        <button
          :id="sectionElementId(group.path)"
          type="button"
          :class="{ 'workspace__group-toggle--selected': selectedKey === sectionKey(group.path) }"
          class="workspace__group-toggle"
          :aria-label="group.title"
          :aria-expanded="!group.collapsed"
          :aria-current="selectedKey === sectionKey(group.path) ? 'true' : undefined"
          @click="emit('toggleGroup', group.path)"
          @keydown.down.prevent="emit('move', 1)"
          @keydown.up.prevent="emit('move', -1)"
        >
          <GuiIcon :name="group.collapsed ? 'chevronRight' : 'chevronDown'" class="gui-group-heading__caret" />
          <span class="gui-group-heading__title">{{ group.title }}</span>
          <span class="gui-group-heading__count mono">{{ group.count }}</span>
        </button>
      </component>
      <ul v-if="!group.collapsed && group.children.length === 0" role="listbox" :aria-label="'Resources: ' + group.title">
        <li
          v-for="row in group.rows"
          :id="rowElementId(row.id)"
          :key="row.id"
          role="option"
          tabindex="0"
          :aria-selected="selectedKey === rowKey(row.id)"
          :class="{ 'workspace__row--selected': selectedKey === rowKey(row.id) }"
          class="workspace__row"
          :style="{ gridTemplateColumns: rowColumns }"
          @click="emit('select', row)"
          @keydown.enter.prevent="emit('activate', row)"
          @keydown.space.prevent="emit('select', row)"
          @keydown.down.prevent="emit('move', 1)"
          @keydown.up.prevent="emit('move', -1)"
        >
          <span class="workspace__check" :class="{ 'workspace__check--on': row.checked }" aria-hidden="true">[<span class="workspace__check-mark">{{ row.checked ? '*' : ' ' }}</span>]</span>
          <span class="workspace__kind" :class="{ 'gui-visually-hidden': hideKind }">{{ row.kind }}</span>
          <span class="workspace__name-cell">
            <span class="workspace__name"><template v-for="(part, index) in nameParts(row)" :key="index"><mark v-if="part.matched" class="workspace__name-match">{{ part.text }}</mark><template v-else>{{ part.text }}</template></template></span>
            <span v-if="problemReason(row)" class="gui-tag workspace__reason" :class="`workspace__reason--${problemReason(row)?.tone}`">{{ problemReason(row)?.text }}</span>
            <span v-if="scopeIsTag(row.scope)" class="workspace__scope workspace__scope--tag">{{ row.scope }}</span>
            <span v-else class="workspace__scope gui-visually-hidden">{{ row.scope }}</span>
            <span v-if="row.harnessOverride" class="workspace__badge" title="Registry-level harness override">harnesses: {{ row.harnesses.join(', ') }}</span>
          </span>
          <span class="workspace__registry" :class="{ 'gui-visually-hidden': hideRegistry }" :title="row.registry">{{ row.registry }}</span>
        </li>
      </ul>
    </div>
  </section>
</template>
