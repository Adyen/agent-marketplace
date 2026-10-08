<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { WorkspaceController } from './controller'

const props = defineProps<{ controller: WorkspaceController }>()
const open = defineModel<boolean>('open', { default: false })
const emit = defineEmits<{ cancel: [] }>()

const step = ref<'targets' | 'consent'>('targets')
const draft = ref<Set<string>>(new Set())
const consent = ref<boolean | null>(null)
const pending = ref(false)
const actionError = ref<string | null>(null)
let submitted = false

const configuration = computed(() => props.controller.state.configuration)
const canInitialize = computed(() => configuration.value?.canInitialize === true)
const detectionUnavailable = computed(() => configuration.value?.detectionUnavailable === true)
const needsConsent = computed(() => configuration.value?.consentRequired === true)
const options = computed(() => configuration.value?.supportedTargets ?? [])
const submitting = computed(
  () =>
    pending.value ||
    props.controller.state.operation != null ||
    props.controller.state.requesting,
)

watch(open, (isOpen) => {
  if (!isOpen) {
    if (!submitted) {
      emit('cancel')
    }
    return
  }
  submitted = false
  step.value = 'targets'
  consent.value = null
  actionError.value = null
  draft.value = new Set(configuration.value?.detectedTargets ?? [])
})

function toggle(id: string, checked: boolean | 'indeterminate') {
  const next = new Set(draft.value)
  if (checked === true) {
    next.add(id)
  } else {
    next.delete(id)
  }
  draft.value = next
}

function selectedTargets(): string[] {
  return options.value.filter((id) => draft.value.has(id))
}

function cancel() {
  if (submitting.value) {
    return
  }
  open.value = false
}

async function finish() {
  const targets = selectedTargets()
  if (!canInitialize.value || targets.length === 0 || (needsConsent.value && consent.value === null)) {
    return
  }
  pending.value = true
  const ok = await props.controller.initialize(
    targets,
    needsConsent.value ? consent.value === true : undefined,
  )
  pending.value = false
  if (ok) {
    submitted = true
    open.value = false
    return
  }
  actionError.value = props.controller.state.error ?? 'The action could not be started'
}
</script>

<template>
  <UModal
    v-model:open="open"
    title="Set up Agent Marketplace"
    aria-label="Set up Agent Marketplace"
    :dismissible="!submitting"
  >
    <template #content>
      <section class="settings" aria-label="Set up Agent Marketplace">
        <h2 class="settings__heading">Set up Agent Marketplace</h2>
        <p v-if="actionError" role="alert" class="settings__error">{{ actionError }}</p>
        <p v-if="!canInitialize" role="status">Initialization is not currently available.</p>
        <p v-else-if="detectionUnavailable" role="status">Harness detection is unavailable; select targets manually.</p>

        <template v-if="step === 'targets'">
          <p>Choose the Harness targets this installation manages.</p>
          <fieldset class="settings__group" :disabled="submitting">
            <legend>Harness targets</legend>
            <label v-for="id in options" :key="id" class="settings__option">
              <UCheckbox
                :model-value="draft.has(id)"
                :label="id"
                :aria-label="`Target ${id}`"
                @update:model-value="toggle(id, $event)"
              />
            </label>
          </fieldset>
          <div class="settings__actions">
            <UButton label="Cancel" color="neutral" variant="outline" aria-label="Cancel" :disabled="submitting" @click="cancel" />
            <UButton
              v-if="needsConsent"
              label="Next"
              aria-label="Next"
              :disabled="!canInitialize || draft.size === 0 || submitting"
              @click="step = 'consent'"
            />
            <UButton
              v-else
              label="Finish setup"
              aria-label="Finish setup"
              :disabled="!canInitialize || draft.size === 0 || submitting"
              @click="finish"
            />
          </div>
        </template>

        <template v-else>
          <p>Agent Marketplace can send basic telemetry: CLI version, OS, architecture, command name (not arguments), harness types, outcome, and coarse duration.</p>
          <p>It does not include resource names, URLs, paths, errors, or prompts.</p>
          <p>Project destination: {{ configuration?.telemetryDestination }}</p>
          <p>Retention policy: {{ configuration?.retentionPolicyURL }}</p>
          <fieldset class="settings__group" :disabled="submitting">
            <legend>Enable basic telemetry?</legend>
            <label class="settings__option">
              <input
                type="radio"
                name="am-first-run-consent"
                aria-label="Yes, enable basic telemetry"
                :checked="consent === true"
                @change="consent = true"
              />
              Yes
            </label>
            <label class="settings__option">
              <input
                type="radio"
                name="am-first-run-consent"
                aria-label="No, do not send telemetry"
                :checked="consent === false"
                @change="consent = false"
              />
              No
            </label>
          </fieldset>
          <div class="settings__actions">
            <UButton label="Back" color="neutral" variant="outline" aria-label="Back" :disabled="submitting" @click="step = 'targets'" />
            <UButton label="Cancel" color="neutral" variant="outline" aria-label="Cancel" :disabled="submitting" @click="cancel" />
            <UButton
              label="Finish setup"
              aria-label="Finish setup"
              :disabled="!canInitialize || consent === null || submitting"
              @click="finish"
            />
          </div>
        </template>
      </section>
    </template>
  </UModal>
</template>
