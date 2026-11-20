<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import type { SourceDescription } from './api/client'
import { copyText } from './clipboard'

const props = defineProps<{
  source: SourceDescription
  label: string
  problemCount: number
  canRemove: boolean
}>()

const emit = defineEmits<{
  showProblems: []
  remove: []
}>()

const actions = ref<HTMLDetailsElement | null>(null)
const actionsOpen = ref(false)
const copyStatus = ref('')

const sourceKind = computed(() => {
  switch (props.source.kind) {
    case 'git':
      return 'Git repository'
    case 'directory':
      return 'Folder'
    case 'marketplace':
      return 'Marketplace'
  }
})

const browserURL = computed(() => {
  const location = props.source.location
  if (props.source.kind === 'directory' || !/^https?:\/\//i.test(location) || /[\x00-\x20\x7f?#]/.test(location)) {
    return null
  }
  try {
    const url = new URL(location)
    return (url.protocol === 'http:' || url.protocol === 'https:') && !url.username && !url.password ? url.href : null
  } catch {
    return null
  }
})

function closeActions(restoreFocus = true) {
  const details = actions.value
  if (details == null || !details.open) {
    return
  }
  details.open = false
  actionsOpen.value = false
  if (restoreFocus) {
    void nextTick(() => details.querySelector<HTMLElement>('summary')?.focus())
  }
}

function updateActionsOpen(event: Event) {
  actionsOpen.value = (event.currentTarget as HTMLDetailsElement).open
}

async function copyLocation() {
  closeActions()
  copyStatus.value = ''
  try {
    copyStatus.value = await copyText(props.source.location) ? 'Location copied' : 'Could not copy location'
  } catch {
    copyStatus.value = 'Could not copy location'
  }
}

function openLocation() {
  const url = browserURL.value
  if (url == null) {
    return
  }
  closeActions()
  const runtime = window.runtime
  if (runtime?.BrowserOpenURL != null) {
    runtime.BrowserOpenURL(url)
    return
  }
  window.open(url, '_blank', 'noopener,noreferrer')
}

function removeSource() {
  closeActions(false)
  emit('remove')
}
</script>

<template>
  <section class="source-browse-header" aria-label="Selected source">
    <div class="source-browse-header__identity">
      <div class="source-browse-header__title">
        <h2>{{ label }}</h2>
        <span class="gui-tag">{{ sourceKind }}</span>
      </div>
      <p class="source-browse-header__location">{{ source.location }}</p>
      <div v-if="source.ref !== '' || source.path !== ''" class="source-browse-header__declared">
        <span v-if="source.ref !== ''">Ref {{ source.ref }}</span>
        <span v-if="source.path !== ''">Subpath {{ source.path }}</span>
      </div>
    </div>
    <div class="source-browse-header__actions">
      <button
        v-if="problemCount > 0"
        type="button"
        class="source-browse-header__problem-action"
        aria-label="View source problems"
        @click="emit('showProblems')"
      >View source problems ({{ problemCount }})</button>
      <span class="source-browse-header__copy-status" role="status" aria-live="polite">{{ copyStatus }}</span>
      <details ref="actions" class="source-browse-header__menu" @toggle="updateActionsOpen" @keydown.esc.prevent.stop="closeActions(true)">
        <summary role="button" aria-label="Source actions" :aria-expanded="actionsOpen">Source actions</summary>
        <div class="source-browse-header__menu-items">
          <button v-if="browserURL != null" type="button" @click="openLocation">Open in browser</button>
          <button type="button" @click="copyLocation">Copy location</button>
          <button v-if="canRemove" type="button" class="source-browse-header__remove" @click="removeSource">Remove source</button>
        </div>
      </details>
    </div>
  </section>
</template>
