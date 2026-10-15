<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Snapshot } from './api/client'
import type { Diagnostic, Row } from './controller'
import GuiIcon from './GuiIcon.vue'

const props = defineProps<{
  folded: boolean
  selected: Row | null
  drift: string
  harnesses: string[]
  discoveryEnabled: boolean
  discoveryLoading: boolean
  discoveryError: string | null
  discoveryHealth: Snapshot['discoveryHealth']
  discoveryFailed: boolean
  diagnostics: Diagnostic[]
  actionDisabled: boolean
  overlay: boolean
}>()

const emit = defineEmits<{
  openResourceAction: [row: Row]
  openInspection: [row: Row]
  retryDiscovery: []
}>()

type ButtonRef = { $el: HTMLElement } | null
const openResourceButton = ref<ButtonRef>(null)
const resourceActionButton = ref<ButtonRef>(null)

const resourceActionLabel = computed(() => {
  switch (props.selected?.resourceAction) {
    case 'install':
      return 'Install Resource'
    case 'remove':
      return 'Remove Resource'
    case 'exclude':
      return 'Exclude Resource'
    case 'restore':
      return 'Restore Resource'
    default:
      return 'Resource action'
  }
})

defineExpose({
  focusOpenResource: () => openResourceButton.value?.$el.focus(),
  focusResourceAction(): boolean {
    const button = resourceActionButton.value?.$el
    if (!(button instanceof HTMLButtonElement) || button.disabled) {
      return false
    }
    button.focus()
    return true
  },
})
</script>

<template>
  <aside
    aria-label="Detail"
    class="workspace__detail gui-inspector"
    :class="{ 'gui-inspector--overlay': overlay && !folded, 'gui-inspector--folded': folded }"
  >
    <header class="gui-inspector__header">
      <div class="gui-pane-title">Details</div>
    </header>
    <div class="gui-inspector__body">
      <section class="gui-inspector__section">
        <template v-if="selected">
          <h2 class="gui-inspector__title mono">{{ selected.registry }}/{{ selected.kind }}/{{ selected.displayName }}</h2>
          <p class="gui-inspector__description">{{ selected.description }}</p>
          <dl>
            <dt>Registry</dt>
            <dd class="mono">{{ selected.registry }}</dd>
            <dt>Scope</dt>
            <dd>{{ selected.scope }}</dd>
            <dt>Status</dt>
            <dd>{{ selected.status }}</dd>
            <dt>Harnesses</dt>
            <dd class="mono">{{ selected.harnesses.join(', ') || 'none' }}</dd>
          </dl>
          <div class="gui-inspector__actions">
            <UButton
              v-if="selected.resourceAction !== 'none'"
              ref="resourceActionButton"
              :aria-label="resourceActionLabel"
              :disabled="actionDisabled"
              @click="emit('openResourceAction', selected)"
            >{{ resourceActionLabel }}</UButton>
            <UButton
              v-if="selected.inspectable || selected.installedFiles"
              ref="openResourceButton"
              aria-label="Open resource"
              color="neutral"
              variant="outline"
              @click="emit('openInspection', selected)"
            >Open resource</UButton>
          </div>
        </template>
        <p v-else class="gui-inspector__empty">Select a resource to inspect it.</p>
      </section>

      <section class="gui-inspector__section">
        <h2 class="gui-caps">Session</h2>
        <p>Drift: {{ drift }}</p>
        <p>Harnesses: <span class="mono">{{ harnesses.join(', ') || 'none' }}</span></p>
        <section v-if="discoveryEnabled" role="region" aria-label="Marketplace discovery health" class="workspace__discovery-health">
          <h2 class="gui-caps">Marketplace discovery</h2>
          <p v-if="discoveryLoading" role="status" aria-live="polite">Marketplace discovery is updating.</p>
          <p v-if="discoveryError" role="status">{{ discoveryError }}</p>
          <button v-if="discoveryFailed" type="button" aria-label="Retry Marketplace search" @click="emit('retryDiscovery')">Retry Marketplace search</button>
          <ul v-if="discoveryHealth.length" aria-label="Marketplace endpoint health">
            <li v-for="health in discoveryHealth" :key="health.id">
              <p><strong>{{ health.label }}</strong> · {{ health.cacheState }} · {{ health.total }} {{ health.total === 1 ? 'result' : 'results' }}<span v-if="health.truncated"> · truncated</span></p>
              <p v-if="health.message">{{ health.message }}</p>
            </li>
          </ul>
        </section>
      </section>

      <section class="gui-inspector__section">
        <h2 class="gui-caps">Diagnostics</h2>
        <ul v-if="diagnostics.length > 0" aria-label="Diagnostics" class="gui-diagnostics">
          <li v-for="d in diagnostics" :key="d.code" :class="`diag--${d.severity}`">
            <GuiIcon :name="d.severity === 'error' ? 'error' : d.severity === 'warning' ? 'warning' : 'info'" class="gui-diagnostics__icon" />
            <span>{{ d.severity }}: {{ d.message }}</span>
          </li>
        </ul>
        <p v-else>None</p>
      </section>
    </div>
  </aside>
</template>
