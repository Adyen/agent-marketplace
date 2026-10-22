<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import GuiIcon from './GuiIcon.vue'
import type { ViewSettings } from './grouping'
import ViewMenu from './ViewMenu.vue'

const props = defineProps<{
  sidebarFolded: boolean
  inspectorFolded: boolean
  searchEditing: boolean
  draftQuery: string
  activeQuery: string
  appliedQuery: string
  localMatchCount: number
  busy: boolean
  ready: boolean
  pendingCount: number
  showSetup: boolean
  viewSettings: ViewSettings
  title: string
}>()

const emit = defineEmits<{
  toggleSidebar: []
  toggleInspector: []
  openSearch: []
  searchInput: [query: string]
  confirmSearch: []
  cancelSearch: []
  clearSearch: []
  changeView: [settings: ViewSettings]
  refresh: []
  sync: []
  update: []
  setup: []
}>()

const sidebarToggle = ref<HTMLButtonElement | null>(null)
const inspectorToggle = ref<HTMLButtonElement | null>(null)
const searchButton = ref<HTMLButtonElement | null>(null)
const searchInput = ref<HTMLInputElement | null>(null)
const setupButton = ref<{ $el: HTMLElement } | null>(null)
const viewMenu = ref<InstanceType<typeof ViewMenu> | null>(null)

watch(
  () => props.searchEditing,
  () => {
    void nextTick(() => searchInput.value?.focus())
  },
)

function onSearchInput(event: Event) {
  emit('searchInput', (event.currentTarget as HTMLInputElement).value)
}

defineExpose({
  focusSearchButton: () => searchButton.value?.focus(),
  focusSearchInput: () => searchInput.value?.focus(),
  focusSidebarToggle: () => sidebarToggle.value?.focus(),
  focusInspectorToggle: () => inspectorToggle.value?.focus(),
  openViewMenu: () => viewMenu.value?.open(),
  focusSetup(): boolean {
    const element = setupButton.value?.$el
    if (element instanceof HTMLElement) {
      element.focus()
      return true
    }
    return false
  },
})
</script>

<template>
  <header class="gui-toolbar">
    <div class="gui-toolbar__leading">
      <button
        v-if="sidebarFolded"
        ref="sidebarToggle"
        type="button"
        class="gui-icon-button"
        aria-label="Toggle sidebar"
        aria-expanded="false"
        @click="emit('toggleSidebar')"
      ><GuiIcon name="sidebar" /></button>
      <h1 class="gui-pane-title">{{ title }}</h1>
    </div>
    <div class="workspace__actions gui-toolbar__actions" role="toolbar" aria-label="Session actions">
      <div v-if="searchEditing" class="gui-search-field">
        <GuiIcon name="search" />
        <input
          id="workspace-search"
          ref="searchInput"
          class="gui-input gui-input--toolbar workspace__search-input"
          type="search"
          aria-label="Search resources"
          :value="draftQuery"
          @input="onSearchInput"
          @keydown.enter.prevent="emit('confirmSearch')"
          @keydown.esc.prevent="emit('cancelSearch')"
        />
      </div>
      <button
        v-else
        ref="searchButton"
        type="button"
        class="gui-search-pill"
        :class="{ 'gui-search-pill--applied': activeQuery !== '' }"
        aria-label="Search"
        :aria-expanded="searchEditing"
        @click="emit('openSearch')"
      >
        <GuiIcon name="search" />
        <span v-if="activeQuery !== ''" class="gui-search-pill__query">{{ activeQuery }}</span>
        <span v-if="activeQuery !== ''" class="mono">· {{ localMatchCount }}</span>
        <template v-else>
          <span>Search</span>
          <kbd class="gui-kbd">⌘K</kbd>
        </template>
      </button>
      <span v-if="activeQuery !== ''" class="workspace__search-status gui-visually-hidden" role="status" aria-live="polite">
        {{ activeQuery }} · {{ localMatchCount }} local {{ localMatchCount === 1 ? 'match' : 'matches' }}
      </span>
      <button
        v-if="activeQuery !== '' || appliedQuery !== '' || draftQuery !== ''"
        type="button"
        class="gui-icon-button"
        aria-label="Clear search"
        @click="emit('clearSearch')"
      ><GuiIcon name="close" /></button>
      <ViewMenu ref="viewMenu" :settings="viewSettings" @change="emit('changeView', $event)" />
      <button type="button" class="gui-icon-button" aria-label="Refresh" :disabled="busy" @click="emit('refresh')"><GuiIcon name="refresh" /></button>
      <UButton aria-label="Sync" :disabled="!ready" @click="emit('sync')">
        <GuiIcon name="sync" />
        Sync
        <span v-if="pendingCount > 0" class="gui-badge mono">~{{ pendingCount }}</span>
      </UButton>
      <UButton aria-label="Update" class="gui-button--secondary" color="neutral" variant="soft" :disabled="!ready" @click="emit('update')">
        <GuiIcon name="update" />
        Update
      </UButton>
      <UButton
        v-if="showSetup"
        ref="setupButton"
        aria-label="Set up"
        color="neutral"
        variant="outline"
        @click="emit('setup')"
      >Set up</UButton>
      <span class="gui-divider" aria-hidden="true" />
      <button
        ref="inspectorToggle"
        type="button"
        class="gui-icon-button"
        aria-label="Toggle inspector"
        :aria-expanded="!inspectorFolded"
        @click="emit('toggleInspector')"
      ><GuiIcon name="inspector" /></button>
    </div>
  </header>
</template>
