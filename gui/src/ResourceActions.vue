<script setup lang="ts">
import { computed } from 'vue'
import type { ResourceAction } from './api/client'
import { eligible, type WorkspaceController } from './controller'
import GuiModal from './GuiModal.vue'

const props = defineProps<{ controller: WorkspaceController }>()
const open = defineModel<boolean>('open', { default: false })

const state = computed(() => props.controller.state.resourceAction)
const row = computed(() => props.controller.state.rows.find((candidate) => candidate.id === state.value?.rowId) ?? null)
const action = computed<ResourceAction>(() => state.value?.plan?.action ?? row.value?.resourceAction ?? 'none')
const targetNames = computed(() => {
  const targets = row.value?.harnesses ?? []
  return targets.length > 0 ? targets.map(harnessDisplayName).join(', ') : 'your configured tools'
})
const actionCopy = computed(() => {
  const name = row.value?.displayName ?? row.value?.name ?? 'this resource'
  const registry = row.value?.registry ?? 'its source'
  switch (action.value) {
    case 'install':
    case 'restore':
      return `${actionName(action.value)} ${name} from ${registry}. After the next sync, it will be available in ${targetNames.value}.`
    case 'remove':
      return `Remove ${name} from ${registry}. After the next sync, it will no longer be available in ${targetNames.value}.`
    case 'exclude':
      return `Exclude ${name} from ${registry}. It will not be available in ${targetNames.value}.`
    default:
      return ''
  }
})

function actionName(value: ResourceAction): string {
  switch (value) {
    case 'install':
      return 'Install'
    case 'remove':
      return 'Remove'
    case 'exclude':
      return 'Exclude'
    case 'restore':
      return 'Restore'
    default:
      return 'Resource action'
  }
}

const title = computed(() => `${actionName(action.value)} Resource`)
const confirmLabel = computed(() => `Confirm ${actionName(action.value).toLowerCase()}`)
const showConfirm = computed(() => state.value?.error == null || state.value.plan != null)
const canConfirm = computed(() => {
  const current = state.value
  const plan = current?.plan
  return (
    current != null &&
    plan != null &&
    !current.loading &&
    !props.controller.resourceActionSubmitting &&
    props.controller.state.operation == null &&
    !props.controller.state.requesting &&
    props.controller.state.configuration?.editable === true &&
    eligible(props.controller.state) &&
    (!plan.requiresRiskAcceptance || current.acceptRisk)
  )
})

function setRiskAcceptance(event: Event) {
  props.controller.setResourceActionRiskAcceptance((event.currentTarget as HTMLInputElement).checked)
}

function close() {
  open.value = false
}

function harnessDisplayName(id: string): string {
  if (id === 'opencode') return 'OpenCode'
  return id.replace(/(^|-)([a-z])/g, (_, separator: string, letter: string) => `${separator === '' ? '' : ' '}${letter.toUpperCase()}`)
}
</script>

<template>
  <GuiModal
    v-model:open="open"
    :title="title"
    :dismissible="!controller.resourceActionSubmitting"
  >
    <div class="resource-actions">
      <p v-if="state?.loading || controller.resourceActionSubmitting" class="resource-actions__status" role="status" aria-live="polite">
        <span class="gui-toast__spinner resource-actions__spinner" aria-hidden="true" />
        <span>{{ state?.plan == null ? 'Preparing a verified resource action…' : 'Waiting for the resource action to be accepted…' }}</span>
      </p>
      <p class="resource-actions__copy">{{ actionCopy }}</p>
      <p v-if="state?.error" role="alert">{{ state.error }}</p>
      <details v-if="state?.plan" class="resource-actions__details">
        <summary>Technical details</summary>
        <pre class="resource-actions__preview" role="region" tabindex="0" aria-label="Resource action preview">{{ state.plan.preview }}</pre>
      </details>
      <label v-if="state?.plan?.requiresRiskAcceptance" class="resource-actions__risk">
        <input
          type="checkbox"
          :checked="state.acceptRisk"
          :disabled="state.loading || controller.resourceActionSubmitting"
          @change="setRiskAcceptance"
        />
        <span>I acknowledge the risk identified in this Marketplace install.</span>
      </label>
    </div>
    <template #footer>
      <UButton label="Cancel" aria-label="Cancel" color="neutral" variant="outline" :disabled="controller.resourceActionSubmitting" @click="close" />
      <UButton
        v-if="state?.error"
        label="Retry plan"
        aria-label="Retry resource action plan"
        color="neutral"
        variant="outline"
        :disabled="state.loading || controller.resourceActionSubmitting"
        @click="controller.retryResourceActionPlan()"
      />
      <UButton
        v-if="showConfirm"
        :label="confirmLabel"
        :aria-label="confirmLabel"
        :disabled="!canConfirm"
        @click="controller.confirmResourceAction()"
      />
    </template>
  </GuiModal>
</template>
