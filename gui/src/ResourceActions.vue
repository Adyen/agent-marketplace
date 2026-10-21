<script setup lang="ts">
import { computed } from 'vue'
import type { ResourceAction } from './api/client'
import { eligible, type WorkspaceController } from './controller'
import type { IconName } from './GuiIcon.vue'
import GuiModal from './GuiModal.vue'

const props = defineProps<{ controller: WorkspaceController }>()
const open = defineModel<boolean>('open', { default: false })

const state = computed(() => props.controller.state.resourceAction)
const row = computed(() => props.controller.state.rows.find((candidate) => candidate.id === state.value?.rowId) ?? null)
const action = computed<ResourceAction>(() => state.value?.plan?.action ?? row.value?.resourceAction ?? 'none')

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
const iconName = computed<IconName>(() => (action.value === 'install' || action.value === 'restore' ? 'plus' : 'minus'))
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
</script>

<template>
  <GuiModal
    v-model:open="open"
    :title="title"
    :icon="iconName"
    :dismissible="!controller.resourceActionSubmitting"
  >
    <div class="resource-actions">
      <p v-if="state?.loading && state.plan == null" role="status" aria-live="polite">Preparing a verified resource action…</p>
      <p v-else-if="state?.loading" role="status" aria-live="polite">Waiting for the resource action to be accepted…</p>
      <p v-if="state?.error" role="alert">{{ state.error }}</p>
      <pre v-if="state?.plan" class="resource-actions__preview" role="region" tabindex="0" aria-label="Resource action preview">{{ state.plan.preview }}</pre>
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
      <UButton
        v-if="showConfirm"
        :label="confirmLabel"
        :aria-label="confirmLabel"
        :disabled="!canConfirm"
        @click="controller.confirmResourceAction()"
      />
      <UButton
        v-if="state?.error"
        label="Retry plan"
        aria-label="Retry resource action plan"
        color="neutral"
        variant="outline"
        :disabled="state.loading || controller.resourceActionSubmitting"
        @click="controller.retryResourceActionPlan()"
      />
      <UButton label="Cancel" aria-label="Cancel" color="neutral" variant="outline" :disabled="controller.resourceActionSubmitting" @click="close" />
    </template>
  </GuiModal>
</template>
