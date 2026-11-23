<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import type { SourceInput } from './api/client'
import type { WorkspaceController } from './controller'
import GuiModal from './GuiModal.vue'
import GuiIcon from './GuiIcon.vue'
import SourceForm from './SourceForm.vue'

const props = defineProps<{ controller: WorkspaceController }>()
const open = defineModel<boolean>('open', { default: false })

const formEpoch = ref(0)
const pending = ref(false)
const failures = ref<string[]>([])
let awaitingSettle = false

const serverLocationError = computed(() => {
  if (props.controller.state.sourceErrorField !== 'location') return undefined
  return props.controller.state.sourceError ?? undefined
})
const serverError = computed(() => {
  if (props.controller.state.sourceErrorField === 'location') return undefined
  return props.controller.state.sourceError ?? undefined
})

let opener: HTMLElement | null = null

watch(open, (isOpen) => {
  if (isOpen) opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
  if (!isOpen) {
    props.controller.state.addSourceBaseline = null
    const returnTo = opener
    opener = null
    setTimeout(() => {
      const target = returnTo?.isConnected && !(returnTo as HTMLButtonElement).disabled
        ? returnTo
        : document.querySelector<HTMLElement>('[role="dialog"] [role="tab"][aria-selected="true"]')
      target?.focus()
    }, 0)
    return
  }
  props.controller.state.sourceError = null
  props.controller.state.sourceErrorField = null
  failures.value = []
  awaitingSettle = false
  formEpoch.value += 1
  void nextTick(() => document.querySelector<HTMLInputElement>('.gui-modal input[aria-label="Source location"]')?.focus())
}, { flush: 'sync' })

function clearLocationError() {
  if (props.controller.state.sourceErrorField === 'location') {
    props.controller.state.sourceError = null
    props.controller.state.sourceErrorField = null
  }
}

function settled() {
  return props.controller.state.operation == null && !props.controller.state.requesting
}

function finish() {
  awaitingSettle = false
  pending.value = false
  const errors = props.controller.state.diagnostics.filter(
    (d) => d.severity === 'error' && d.category === 'mutation' && !(props.controller.state.addSourceBaseline ?? []).includes(d),
  )
  failures.value = errors.map((d) => d.message)
  if (errors.length > 0) return
  props.controller.state.addSourceBaseline = null
  open.value = false
}

watch(
  () => [props.controller.state.operation, props.controller.state.requesting, props.controller.state.diagnostics] as const,
  () => {
    if (awaitingSettle && settled()) finish()
  },
)

async function add(source: SourceInput) {
  pending.value = true
  const accepted = await props.controller.addSource(source)
  if (!accepted) {
    pending.value = false
    return
  }
  awaitingSettle = true
  if (settled()) finish()
}
</script>

<template>
  <GuiModal v-model:open="open" title="Add source" icon="plus" size="sm" :busy="pending" :dismissible="!pending">
    <div class="add-source-modal">
      <SourceForm
        :key="formEpoch"
        :disabled="pending"
        :busy="pending"
        :server-location-error="serverLocationError"
        @add="add"
        @edit-location="clearLocationError"
      />
      <p v-if="serverError" role="alert" class="settings__notice settings__notice--danger">
        <GuiIcon name="error" class="settings__notice-icon" />
        <span>{{ serverError }}</span>
      </p>
      <p v-for="message in failures" :key="message" role="alert" class="settings__notice settings__notice--danger">
        <GuiIcon name="error" class="settings__notice-icon" />
        <span>{{ message }}</span>
      </p>
    </div>
  </GuiModal>
</template>
