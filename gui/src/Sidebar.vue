<script setup lang="ts">
import { computed, ref } from 'vue'
import type { SourceDescription } from './api/client'
import GuiIcon from './GuiIcon.vue'

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
  sources: SidebarSource[]
  harnesses: string[]
  latestVersion: string
}>()

const emit = defineEmits<{
  toggle: []
  search: []
  showAll: []
  selectGroup: [id: string]
  openSources: []
  openSettings: []
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
      <button type="button" class="gui-nav-row" aria-label="Show all groups" @click="emit('showAll')">
        <GuiIcon name="folder" />
        <span class="gui-nav-row__label">Resources</span>
      </button>
      <button type="button" class="gui-nav-row" aria-label="Pending sync group" @click="emit('selectGroup', 'pending')">
        <GuiIcon name="sync" />
        <span class="gui-nav-row__label">Pending sync</span>
        <span class="gui-nav-row__count">{{ pendingCount }}</span>
      </button>
      <button type="button" class="gui-nav-row" aria-label="Open search" aria-keyshortcuts="Meta+K" @click="emit('search')">
        <GuiIcon name="search" />
        <span class="gui-nav-row__label">Search</span>
        <kbd class="gui-kbd">⌘K</kbd>
      </button>
    </nav>

    <section class="gui-sidebar__section" aria-label="Groups">
      <h2 class="gui-caps">Groups</h2>
      <ul class="gui-sidebar__list">
        <li v-for="group in groups" :key="group.id">
          <button
            type="button"
            class="gui-nav-row gui-nav-row--compact"
            :aria-label="`Show ${group.title} group`"
            @click="emit('selectGroup', group.id)"
          >
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
            <GuiIcon name="folder" />
            <span class="gui-source-row__location">{{ entry.source.location }}</span>
            <span v-if="entry.organization" class="gui-tag">ORG</span>
          </button>
        </li>
      </ul>
    </section>

    <footer class="gui-sidebar__footer">
      <p class="gui-sidebar__harnesses mono">{{ harnessSummary || 'No harnesses' }}</p>
      <p v-if="latestVersion !== ''" class="gui-sidebar__update">
        <span class="gui-dot" aria-hidden="true" />
        <span>v{{ latestVersion }} available</span>
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
    </footer>
  </aside>
</template>
