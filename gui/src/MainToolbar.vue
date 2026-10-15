<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import GuiIcon from './GuiIcon.vue'

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
}>()

const emit = defineEmits<{
  toggleSidebar: []
  toggleInspector: []
  openSearch: []
  searchInput: [query: string]
  confirmSearch: []
  cancelSearch: []
  clearSearch: []
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
      <h1 class="gui-pane-title">Resources</h1>
    </div>
    <div class="workspace__actions gui-toolbar__actions" role="toolbar" aria-label="Session actions">
      <button
        ref="searchButton"
        type="button"
        class="gui-search-pill"
        aria-label="Search"
        :aria-expanded="searchEditing"
        @click="emit('openSearch')"
      >
        <GuiIcon name="search" />
        <span v-if="!searchEditing">Search</span>
        <kbd v-if="!searchEditing" class="gui-kbd">⌘K</kbd>
      </button>
      <input
        v-if="searchEditing"
        id="workspace-search"
        ref="searchInput"
        class="workspace__search-input"
        type="search"
        aria-label="Search resources"
        :value="draftQuery"
        @input="onSearchInput"
        @keydown.enter.prevent="emit('confirmSearch')"
        @keydown.esc.prevent="emit('cancelSearch')"
      />
      <span v-if="activeQuery !== ''" class="workspace__search-status" role="status" aria-live="polite">
        {{ activeQuery }} · {{ localMatchCount }} local {{ localMatchCount === 1 ? 'match' : 'matches' }}
      </span>
      <button
        v-if="activeQuery !== '' || appliedQuery !== '' || draftQuery !== ''"
        type="button"
        class="gui-icon-button"
        aria-label="Clear search"
        @click="emit('clearSearch')"
      ><GuiIcon name="close" /></button>
      <button type="button" class="gui-icon-button" aria-label="Refresh" :disabled="busy" @click="emit('refresh')"><GuiIcon name="refresh" /></button>
      <UButton aria-label="Sync" :disabled="!ready" @click="emit('sync')">
        <GuiIcon name="sync" />
        Sync
        <span v-if="pendingCount > 0" class="gui-badge mono">~{{ pendingCount }}</span>
      </UButton>
      <UButton aria-label="Update" color="neutral" variant="outline" :disabled="!ready" @click="emit('update')">
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
