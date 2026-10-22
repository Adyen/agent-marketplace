<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Snapshot } from './api/client'
import type { Diagnostic } from './controller'
import { groupDiagnostics } from './diagnostics'
import GuiIcon from './GuiIcon.vue'

const props = defineProps<{
  drift: string
  discoveryEnabled: boolean
  discoveryLoading: boolean
  discoveryError: string | null
  discoveryHealth: Snapshot['discoveryHealth']
  discoveryFailed: boolean
  diagnostics: Diagnostic[]
}>()

const emit = defineEmits<{
  retryDiscovery: []
}>()

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
</script>

<template>
  <div class="gui-diagnostics-view">
    <section class="gui-diagnostics-view__card" aria-label="Session">
      <h2 class="gui-caps">Session</h2>
      <p class="gui-kv">
        <span v-if="driftTone" class="gui-dot" :class="`gui-dot--${driftTone}`" aria-hidden="true" /><span class="gui-kv__key">Drift:</span> <span class="mono">{{ drift }}</span>
      </p>
    </section>

    <section v-if="discoveryEnabled" role="region" aria-label="Marketplace discovery health" class="gui-diagnostics-view__card workspace__discovery-health">
      <h2 class="gui-caps">Marketplace discovery</h2>
      <p v-if="discoveryLoading" role="status" aria-live="polite">Marketplace discovery is updating.</p>
      <p v-if="discoveryError" role="status">{{ discoveryError }}</p>
      <button v-if="discoveryFailed" type="button" aria-label="Retry Marketplace search" @click="emit('retryDiscovery')">Retry Marketplace search</button>
      <ul v-if="discoveryHealth.length" aria-label="Marketplace endpoint health">
        <li v-for="health in discoveryHealth" :key="health.id">
          <p><strong>{{ health.label }}</strong><span class="mono gui-diagnostics-view__meta"> · {{ health.cacheState }} · {{ health.total }} {{ health.total === 1 ? 'result' : 'results' }}<span v-if="health.truncated"> · truncated</span></span></p>
          <p v-if="health.message">{{ health.message }}</p>
        </li>
      </ul>
    </section>

    <section class="gui-diagnostics-view__card">
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
      <p v-else class="gui-diagnostics-view__empty"><GuiIcon name="check" /> No problems found.</p>
    </section>
  </div>
</template>
