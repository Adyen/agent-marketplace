<script setup lang="ts">
import { computed, ref } from 'vue'
import type { SourceDescription } from './api/client'
import GuiIcon, { type IconName } from './GuiIcon.vue'
import { sourceLabel } from './source-label'

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
  folded: boolean
  overlay: boolean
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
  toggle: []
  search: []
  showAll: []
  selectGroup: [id: string]
  openSources: []
  openSettings: []
  showDiagnostics: []
  quit: []
}>()

const isMac = typeof navigator !== 'undefined' && /Mac/.test(navigator.platform)
const toggleButton = ref<HTMLButtonElement | null>(null)
const sourcesButton = ref<HTMLButtonElement | null>(null)
const settingsButton = ref<HTMLButtonElement | null>(null)

defineExpose({
  focusToggle: () => toggleButton.value?.focus(),
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

function sourceName(source: SourceDescription): string {
  const rowLabel = source.rowSourceId === '' ? undefined : props.rowSourceLabels.get(source.rowSourceId)
  return rowLabel || sourceLabel(source)
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
    :class="{ 'gui-sidebar--overlay': overlay && !folded, 'gui-sidebar--folded': folded, 'gui-sidebar--mac': isMac }"
  >
    <div class="gui-sidebar__top">
      <button
        v-if="!folded"
        ref="toggleButton"
        type="button"
        class="gui-icon-button"
        aria-label="Toggle sidebar"
        aria-expanded="true"
        @click="emit('toggle')"
      ><GuiIcon name="sidebar" /></button>
    </div>

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

    <section class="gui-sidebar__section" aria-label="Sources list">
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
      <ul class="gui-sidebar__list">
        <li v-for="entry in sources" :key="entry.source.id">
          <button
            type="button"
            class="gui-nav-row gui-nav-row--compact gui-source-row"
            :title="entry.source.location"
            :aria-label="`Open source ${entry.source.location}`"
            @click="emit('openSources')"
          >
            <GuiIcon :name="sourceIcons[entry.source.kind]" />
            <span class="gui-source-row__location">{{ sourceName(entry.source) }}</span>
            <span v-if="entry.organization" class="gui-tag">ORG</span>
          </button>
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
