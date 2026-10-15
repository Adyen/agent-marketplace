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
import type { Row } from './controller'
import type { NameSegment } from './search'
import GuiIcon from './GuiIcon.vue'

export interface ResourceGroup {
  id: string
  title: string
  collapsed: boolean
  rows: Row[]
}

const props = defineProps<{
  groups: ResourceGroup[]
  selectedKey: string | null
  nameParts: (row: Row) => NameSegment[]
}>()

const emit = defineEmits<{
  toggleGroup: [id: string]
  select: [row: Row]
  activate: [row: Row]
  move: [direction: 1 | -1]
}>()
</script>

<template>
  <section aria-label="Resources" class="workspace__list gui-list">
    <div
      v-for="group in props.groups"
      :key="group.id"
      class="workspace__group"
    >
      <h2>
        <button
          :id="sectionElementId(group.id)"
          type="button"
          :class="{ 'workspace__group-toggle--selected': selectedKey === sectionKey(group.id) }"
          class="workspace__group-toggle"
          :aria-label="group.title"
          :aria-expanded="!group.collapsed"
          :aria-current="selectedKey === sectionKey(group.id) ? 'true' : undefined"
          @click="emit('toggleGroup', group.id)"
          @keydown.down.prevent="emit('move', 1)"
          @keydown.up.prevent="emit('move', -1)"
        >
          <GuiIcon :name="group.collapsed ? 'chevronRight' : 'chevronDown'" class="gui-group-heading__caret" />
          <span class="gui-group-heading__title">{{ group.title }}</span>
          <span class="gui-group-heading__count mono">{{ group.rows.length }}</span>
        </button>
      </h2>
      <ul v-if="!group.collapsed" role="listbox" :aria-label="'Resources: ' + group.title">
        <li
          v-for="row in group.rows"
          :id="rowElementId(row.id)"
          :key="row.id"
          role="option"
          tabindex="0"
          :aria-selected="selectedKey === rowKey(row.id)"
          :class="{ 'workspace__row--selected': selectedKey === rowKey(row.id) }"
          class="workspace__row"
          @click="emit('select', row)"
          @keydown.enter.prevent="emit('activate', row)"
          @keydown.space.prevent="emit('select', row)"
          @keydown.down.prevent="emit('move', 1)"
          @keydown.up.prevent="emit('move', -1)"
        >
          <span class="workspace__check" :class="{ 'workspace__check--on': row.checked }" aria-hidden="true">{{ row.checked ? '[x]' : '[ ]' }}</span>
          <span class="workspace__name">
            <span>{{ row.registry }}/{{ row.kind }}/</span><template v-for="(part, index) in nameParts(row)" :key="index">
              <mark v-if="part.matched" class="workspace__name-match">{{ part.text }}</mark>
              <template v-else>{{ part.text }}</template>
            </template>
          </span>
          <span class="workspace__scope">{{ row.scope }}</span>
          <span v-if="row.harnessOverride" class="workspace__badge" title="Registry-level harness override">harnesses: {{ row.harnesses.join(', ') }}</span>
        </li>
      </ul>
    </div>
  </section>
</template>
