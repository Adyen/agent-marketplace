<script setup lang="ts">
import { computed, ref } from 'vue'
import type { SourceDescription } from './api/client'
import GuiIcon, { type IconName } from './GuiIcon.vue'
import { sourceLabel } from './source-label'
import { flattenSourceGroups, groupSources } from './source-groups'

export interface SidebarGroup {
  id: string
  title: string
  count: number
}

export interface SidebarSource {
  source: SourceDescription
  organization: boolean
}

const props = defineProps<{
  groups: SidebarGroup[]
  pendingCount: number
  pendingSectionId: string
  mainView: 'resources' | 'diagnostics'
  diagnosticsCount: number
  diagnosticsTone: 'danger' | 'warning' | 'info' | null
  sources: SidebarSource[]
  rowSourceLabels: ReadonlyMap<string, string>
  harnesses: string[]
  targetsLoading: boolean
  latestVersion: string
}>()

const emit = defineEmits<{
  search: []
  showAll: []
  selectGroup: [id: string]
  openSources: []
  revealSource: [id: string]
  openSettings: []
  showDiagnostics: []
  quit: []
}>()

const isMac = typeof navigator !== 'undefined' && /Mac/.test(navigator.platform)
const sourcesButton = ref<HTMLButtonElement | null>(null)
const settingsButton = ref<HTMLButtonElement | null>(null)
const foldedSourceGroups = ref(new Set<string>())

defineExpose({
  focusSources: () => sourcesButton.value?.focus(),
  focusSettings: () => settingsButton.value?.focus(),
})

const harnessSummary = computed(() => {
  const shown = props.harnesses.slice(0, 2).join(' · ')
  const hidden = props.harnesses.length - 2
  return hidden > 0 ? `${shown} +${hidden}` : shown
})

const sourceIcons: Record<SourceDescription['kind'], IconName> = {
  directory: 'folder',
  git: 'branch',
  marketplace: 'globe',
}

const sourceEntries = computed(() =>
  flattenSourceGroups(
    groupSources(
      props.sources.map(({ source, organization }) => ({ source, layer: organization ? 'organization' : 'user' })),
    ),
    foldedSourceGroups.value,
  ),
)

function sourceName(source: SourceDescription): string {
  const rowLabel = source.rowSourceId === '' ? undefined : props.rowSourceLabels.get(source.rowSourceId)
  return rowLabel || sourceLabel(source)
}

function toggleSourceGroup(key: string) {
  foldedSourceGroups.value.has(key) ? foldedSourceGroups.value.delete(key) : foldedSourceGroups.value.add(key)
}

function groupTone(group: SidebarGroup): 'warning' | 'danger' | 'signal' | null {
  if (group.count === 0) {
    return null
  }
  switch (group.id) {
    case 'attention':
    case 'malformed':
    case 'stale':
      return 'warning'
    case 'broken':
      return 'danger'
    case 'pending':
      return 'signal'
    default:
      return null
  }
}
</script>

<template>
  <aside
    class="gui-sidebar"
    aria-label="Navigation"
    :class="{ 'gui-sidebar--mac': isMac }"
  >
    <div class="gui-sidebar__top" />

    <nav class="gui-sidebar__nav">
      <button type="button" class="gui-nav-row" aria-label="Show all groups" :aria-current="mainView === 'resources' ? 'page' : undefined" @click="emit('showAll')">
        <GuiIcon name="folder" />
        <span class="gui-nav-row__label">Resources</span>
      </button>
      <button type="button" class="gui-nav-row" aria-label="Pending sync group" @click="emit('selectGroup', pendingSectionId)">
        <GuiIcon name="sync" />
        <span class="gui-nav-row__label">Pending sync</span>
        <span class="gui-nav-row__count">{{ pendingCount }}</span>
      </button>
      <button
        type="button"
        class="gui-nav-row"
        aria-label="Show diagnostics"
        :aria-current="mainView === 'diagnostics' ? 'page' : undefined"
        @click="emit('showDiagnostics')"
      >
        <GuiIcon :name="diagnosticsCount > 0 ? 'warning' : 'info'" />
        <span class="gui-nav-row__label">Diagnostics</span>
        <span class="gui-nav-row__count" :class="diagnosticsTone ? `gui-nav-row__count--${diagnosticsTone}` : ''">{{ diagnosticsCount }}</span>
      </button>
      <button type="button" class="gui-nav-row" aria-label="Open search" aria-keyshortcuts="Meta+K" @click="emit('search')">
        <GuiIcon name="search" />
        <span class="gui-nav-row__label">Search</span>
        <kbd class="gui-kbd">⌘K</kbd>
      </button>
    </nav>

    <section v-if="groups.length > 0" class="gui-sidebar__section" aria-label="Groups">
      <h2 class="gui-caps">Groups</h2>
      <ul class="gui-sidebar__list">
        <li v-for="group in groups" :key="group.id">
          <button
            type="button"
            class="gui-nav-row gui-nav-row--compact"
            :aria-label="`Show ${group.title} group`"
            @click="emit('selectGroup', group.id)"
          >
            <span v-if="groupTone(group)" :class="`gui-dot gui-dot--${groupTone(group)}`" aria-hidden="true" />
            <span class="gui-nav-row__label">{{ group.title }}</span>
            <span class="gui-nav-row__count">{{ group.count }}</span>
          </button>
        </li>
      </ul>
    </section>

    <section class="gui-sidebar__section gui-sidebar__section--scroll" aria-label="Sources list">
      <div class="gui-sidebar__heading">
        <h2 class="gui-caps">Sources</h2>
        <button
          ref="sourcesButton"
          type="button"
          class="gui-icon-button"
          aria-label="Sources"
          @click="emit('openSources')"
        ><GuiIcon name="plus" /></button>
      </div>
      <ul class="gui-sidebar__list gui-sidebar__list--scroll">
        <li v-for="entry in sourceEntries" :key="entry.key" :class="{ 'gui-sidebar__source-child': entry.type === 'source' && entry.groupKey != null }">
          <button
            v-if="entry.type === 'group'"
            type="button"
            class="gui-nav-row gui-nav-row--compact"
            :aria-label="`Source group ${sourceLabel(entry.entry.sources[0])}`"
            :aria-expanded="!foldedSourceGroups.has(entry.key)"
            @click="toggleSourceGroup(entry.key)"
            @keydown.enter.prevent="toggleSourceGroup(entry.key)"
            @keydown.space.prevent="toggleSourceGroup(entry.key)"
          >
            <GuiIcon :name="sourceIcons[entry.entry.kind]" />
            <span class="gui-source-row__location">{{ sourceLabel(entry.entry.sources[0]) }}</span>
            <span class="gui-nav-row__count">{{ entry.entry.sources.length }}</span>
            <span v-if="entry.entry.layer === 'organization'" class="gui-tag">ORG</span>
          </button>
          <UTooltip v-else :text="entry.source.location" :ui="{ content: 'gui-source-row__tooltip' }">
            <button
              type="button"
              class="gui-nav-row gui-nav-row--compact gui-source-row"
              :data-source-id="entry.source.id"
              :aria-label="`Open source ${entry.source.location}`"
              @click="emit('revealSource', entry.source.id)"
            >
              <GuiIcon :name="sourceIcons[entry.source.kind]" />
              <span class="gui-source-row__location">
                {{ entry.groupKey == null ? sourceName(entry.source) : entry.source.path || '/' }}
                <template v-if="entry.groupKey != null && entry.source.ref"> · {{ entry.source.ref }}</template>
              </span>
              <span v-if="entry.layer === 'organization'" class="gui-tag">ORG</span>
            </button>
          </UTooltip>
        </li>
      </ul>
    </section>

    <footer class="gui-sidebar__footer">
      <p v-if="latestVersion !== ''" class="gui-sidebar__update">
        <span class="gui-dot" aria-hidden="true" />
        <span>v{{ latestVersion }} available</span>
      </p>
      <div class="gui-sidebar__footer-row">
        <p class="gui-sidebar__harnesses mono">
          <template v-if="harnessSummary !== ''">
            <span class="gui-dot" aria-hidden="true" />
            <span class="gui-sidebar__harnesses-prefix">targets</span> {{ harnessSummary }}
          </template>
          <template v-else-if="targetsLoading">Loading targets…</template>
          <template v-else>No harnesses</template>
        </p>
        <div class="gui-sidebar__footer-actions">
          <button
            ref="settingsButton"
            type="button"
            class="gui-icon-button"
            aria-label="Settings"
            @click="emit('openSettings')"
          ><GuiIcon name="gear" /></button>
          <button type="button" class="gui-icon-button" aria-label="Quit" @click="emit('quit')">
            <GuiIcon name="quit" />
          </button>
        </div>
      </div>
    </footer>
  </aside>
</template>
