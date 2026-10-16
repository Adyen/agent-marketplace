<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Snapshot } from './api/client'
import type { Diagnostic, Row } from './controller'
import { groupDiagnostics } from './diagnostics'
import GuiIcon from './GuiIcon.vue'

const props = defineProps<{
  folded: boolean
  selected: Row | null
  drift: string
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

const resourceActionVerbs: Partial<Record<Row['resourceAction'], string>> = {
  install: 'Install',
  remove: 'Remove',
  exclude: 'Exclude',
  restore: 'Restore',
}

const resourceActionVerb = computed(() => resourceActionVerbs[props.selected?.resourceAction ?? 'none'] ?? '')

const resourceActionLabel = computed(() => (resourceActionVerb.value ? `${resourceActionVerb.value} Resource` : 'Resource action'))

const resourceActionDanger = computed(() => props.selected?.resourceAction === 'remove' || props.selected?.resourceAction === 'exclude')

const statusTone = computed(() => (props.selected?.status === 'enabled' || props.selected?.status === 'installed' ? 'signal' : 'warning'))

const driftTone = computed(() => {
  switch (props.drift) {
    case 'ready':
      return 'signal'
    case 'pending':
      return 'warning'
    case 'failed':
      return 'danger'
    default:
      return null
  }
})

const diagnosticGroups = computed(() => groupDiagnostics(props.diagnostics))

const expandedDiagnostics = ref(new Set<string>())

function toggleDiagnostic(key: string) {
  const next = new Set(expandedDiagnostics.value)
  if (next.has(key)) {
    next.delete(key)
  } else {
    next.add(key)
  }
  expandedDiagnostics.value = next
}

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
          <div :key="selected.id" class="gui-inspector__resource">
            <p class="gui-caps gui-inspector__eyebrow">{{ selected.kind }}</p>
            <h2 class="gui-inspector__title mono">{{ selected.displayName }}</h2>
            <p class="gui-inspector__description">{{ selected.description }}</p>
            <dl>
              <dt>Registry</dt>
              <dd class="mono">{{ selected.registry }}</dd>
              <dt>Scope</dt>
              <dd>{{ selected.scope }}</dd>
              <dt>Status</dt>
              <dd><span class="gui-dot" :class="`gui-dot--${statusTone}`" aria-hidden="true" />{{ selected.status }}</dd>
              <dt>Harnesses</dt>
              <dd class="mono">{{ selected.harnesses.join(', ') || 'none' }}</dd>
            </dl>
            <div class="gui-inspector__actions">
              <UButton
                v-if="selected.resourceAction !== 'none'"
                ref="resourceActionButton"
                :aria-label="resourceActionLabel"
                :class="['gui-button--secondary', { 'gui-button--danger': resourceActionDanger }]"
                color="neutral"
                variant="soft"
                :disabled="actionDisabled"
                @click="emit('openResourceAction', selected)"
              >{{ resourceActionVerb }}</UButton>
              <UButton
                v-if="selected.inspectable || selected.installedFiles"
                ref="openResourceButton"
                aria-label="Open resource"
                class="gui-button--secondary"
                color="neutral"
                variant="soft"
                @click="emit('openInspection', selected)"
              >Open resource</UButton>
            </div>
          </div>
        </template>
        <div v-else class="gui-inspector__empty">
          <GuiIcon name="file" />
          <p>Select a resource to inspect it.</p>
        </div>
      </section>

      <section class="gui-inspector__section">
        <h2 class="gui-caps">Session</h2>
        <p class="gui-kv">
          <span v-if="driftTone" class="gui-dot" :class="`gui-dot--${driftTone}`" aria-hidden="true" /><span class="gui-kv__key">Drift:</span> <span class="mono">{{ drift }}</span>
        </p>
      </section>

      <section v-if="discoveryEnabled" role="region" aria-label="Marketplace discovery health" class="gui-inspector__section workspace__discovery-health">
        <h2 class="gui-caps">Marketplace discovery</h2>
        <p v-if="discoveryLoading" role="status" aria-live="polite">Marketplace discovery is updating.</p>
        <p v-if="discoveryError" role="status">{{ discoveryError }}</p>
        <button v-if="discoveryFailed" type="button" aria-label="Retry Marketplace search" @click="emit('retryDiscovery')">Retry Marketplace search</button>
        <ul v-if="discoveryHealth.length" aria-label="Marketplace endpoint health">
          <li v-for="health in discoveryHealth" :key="health.id">
            <p><strong>{{ health.label }}</strong><span class="mono gui-inspector__meta"> · {{ health.cacheState }} · {{ health.total }} {{ health.total === 1 ? 'result' : 'results' }}<span v-if="health.truncated"> · truncated</span></span></p>
            <p v-if="health.message">{{ health.message }}</p>
          </li>
        </ul>
      </section>

      <section class="gui-inspector__section">
        <h2 class="gui-caps">Diagnostics<span v-if="diagnostics.length > 0" class="gui-caps__count">{{ diagnostics.length }}</span></h2>
        <ul v-if="diagnostics.length > 0" aria-label="Diagnostics" class="gui-diagnostics">
          <li v-for="group in diagnosticGroups" :key="group.key" :class="`diag--${group.severity}`">
            <button type="button" class="gui-diagnostics__row" :aria-expanded="expandedDiagnostics.has(group.key)" @click="toggleDiagnostic(group.key)">
              <GuiIcon :name="group.severity === 'error' ? 'error' : group.severity === 'warning' ? 'warning' : 'info'" class="gui-diagnostics__icon" />
              <span class="gui-visually-hidden">{{ group.severity }}: </span>
              <span class="gui-diagnostics__message" :class="{ 'gui-diagnostics__message--single': group.messages.length === 1 }">{{ group.messages[0] }}</span>
              <span v-if="group.messages.length > 1" class="gui-tag">×{{ group.messages.length }}</span>
            </button>
            <ul v-if="group.messages.length > 1 && expandedDiagnostics.has(group.key)" class="gui-diagnostics__messages">
              <li v-for="(message, index) in group.messages" :key="index">{{ message }}</li>
            </ul>
          </li>
        </ul>
        <p v-else>None</p>
      </section>
    </div>
  </aside>
</template>
