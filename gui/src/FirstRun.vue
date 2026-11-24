<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import type { SourceDescription, SourceInput } from './api/client'
import type { WorkspaceController } from './controller'
import ChoiceList from './ChoiceList.vue'
import GuiModal from './GuiModal.vue'
import { harnessDisplayName } from './harness-names'
import SourceForm from './SourceForm.vue'
import SourceList from './SourceList.vue'

const props = defineProps<{ controller: WorkspaceController }>()
const open = defineModel<boolean>('open', { default: false })
const emit = defineEmits<{ cancel: [] }>()

const step = ref<'targets' | 'consent' | 'sources'>('targets')
const targetsGroup = ref<HTMLElement | null>(null)
const draft = ref<Set<string>>(new Set())
const consent = ref<boolean | null>(null)
const consentOptions = [
  { value: 'yes', label: 'Share basic telemetry', ariaLabel: 'Yes, enable basic telemetry' },
  { value: 'no', label: "Don't share", ariaLabel: 'No, do not send telemetry' },
]
const consentValue = computed(() => (consent.value === null ? null : consent.value ? 'yes' : 'no'))
const pendingSources = ref<SourceInput[]>([])
const formEpoch = ref(0)
const pending = ref(false)
const actionError = ref<string | null>(null)
let submitted = false

const configuration = computed(() => props.controller.state.configuration)
const canInitialize = computed(() => configuration.value?.canInitialize === true)
const detectionUnavailable = computed(() => configuration.value?.detectionUnavailable === true)
const needsConsent = computed(() => configuration.value?.consentRequired === true)
const showSources = computed(() => (configuration.value?.organizationSources ?? []).length === 0)
const options = computed(() => configuration.value?.supportedTargets ?? [])
const detected = computed(() => new Set(configuration.value?.detectedTargets ?? []))
const submitting = computed(
  () =>
    pending.value ||
    props.controller.state.operation != null ||
    props.controller.state.requesting,
)
const sourceFieldsDisabled = computed(() => submitting.value || !canInitialize.value)
const pendingDescriptions = computed<SourceDescription[]>(() =>
  pendingSources.value.map((source, index) => ({
    id: `draft-${index}`,
    name: '',
    kind: source.kind,
    location: source.location,
    ref: source.kind === 'git' ? (source.ref ?? '') : '',
    path: source.kind === 'git' ? (source.path ?? '') : '',
    sourceHarness: '',
    rowSourceId: '',
  })),
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
  pendingSources.value = []
  formEpoch.value += 1
  draft.value = new Set(configuration.value?.detectedTargets ?? [])
  void nextTick(() => targetsGroup.value?.querySelector<HTMLElement>('[role="checkbox"]')?.focus())
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

function nextFromTargets() {
  step.value = needsConsent.value ? 'consent' : 'sources'
}

function backFromSources() {
  step.value = needsConsent.value ? 'consent' : 'targets'
}

function addPending(source: SourceInput) {
  pendingSources.value = [...pendingSources.value, source]
  formEpoch.value += 1
}

function removePending(id: string) {
  const index = pendingDescriptions.value.findIndex((source) => source.id === id)
  if (index >= 0) {
    pendingSources.value = pendingSources.value.filter((_, i) => i !== index)
  }
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
    pendingSources.value,
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
  <GuiModal v-model:open="open" title="Set up Agent Marketplace" icon="sparkle" size="lg" :dismissible="!submitting">
    <ol class="gui-steps">
      <li :class="{ 'gui-steps__current': step === 'targets' }" :aria-current="step === 'targets' ? 'step' : undefined">1 Targets</li>
      <li :class="{ 'gui-steps__current': step === 'consent' }" :aria-current="step === 'consent' ? 'step' : undefined">2 Telemetry</li>
      <li :class="{ 'gui-steps__current': step === 'sources' }" :aria-current="step === 'sources' ? 'step' : undefined">3 Sources</li>
    </ol>
    <p v-if="actionError" role="alert" class="settings__error">{{ actionError }}</p>
    <p v-if="!canInitialize" role="status">Initialization is not currently available.</p>
    <p v-else-if="detectionUnavailable" role="status">Harness detection is unavailable; select targets manually.</p>

    <template v-if="step === 'targets'">
      <p>Choose which coding agents Agent Marketplace installs resources for.</p>
      <fieldset ref="targetsGroup" class="settings__group" :disabled="submitting">
        <legend>Harness targets</legend>
        <label v-for="id in options" :key="id" class="settings__option">
          <UCheckbox
            :model-value="draft.has(id)"
            :label="harnessDisplayName(id)"
            :aria-label="`Target ${id}`"
            @update:model-value="toggle(id, $event)"
          />
          <span v-if="detected.has(id)" class="gui-tag">Detected</span>
        </label>
      </fieldset>
    </template>

    <template v-else-if="step === 'consent'">
      <p>Agent Marketplace can send basic telemetry: CLI version, OS, architecture, command name (not arguments), harness types, outcome, and coarse duration.</p>
      <p>It does not include resource names, URLs, paths, errors, or prompts.</p>
      <p>Project destination: {{ configuration?.telemetryDestination }}</p>
      <p>Retention policy: {{ configuration?.retentionPolicyURL }}</p>
      <ChoiceList
        name="am-first-run-consent"
        legend="Enable basic telemetry?"
        :options="consentOptions"
        :model-value="consentValue"
        :disabled="submitting"
        @update:model-value="consent = $event === 'yes'"
      />
    </template>

    <template v-else>
      <p>Sources will be added after initialization when Finish setup is pressed.</p>
      <SourceForm :key="formEpoch" :disabled="sourceFieldsDisabled" @add="addPending" />
      <SourceList title="Pending sources" :sources="pendingDescriptions" :organization="false">
        <template #actions="{ source }">
          <UButton
            label="Remove"
            color="neutral"
            variant="outline"
            :aria-label="`Remove source ${source.location}`"
            :disabled="submitting"
            @click="removePending(source.id)"
          />
        </template>
      </SourceList>
    </template>

    <template #footer>
      <UButton label="Cancel" color="neutral" variant="outline" aria-label="Cancel" :disabled="submitting" @click="cancel" />
      <template v-if="step === 'targets'">
        <UButton
          v-if="needsConsent || showSources"
          label="Next"
          aria-label="Next"
          :disabled="!canInitialize || draft.size === 0 || submitting"
          @click="nextFromTargets"
        />
        <UButton
          v-else
          label="Finish setup"
          aria-label="Finish setup"
          :disabled="!canInitialize || draft.size === 0 || submitting"
          @click="finish"
        />
      </template>
      <template v-else-if="step === 'consent'">
        <UButton label="Back" color="neutral" variant="outline" aria-label="Back" :disabled="submitting" @click="step = 'targets'" />
        <UButton
          v-if="showSources"
          label="Next"
          aria-label="Next"
          :disabled="!canInitialize || consent === null || submitting"
          @click="step = 'sources'"
        />
        <UButton
          v-else
          label="Finish setup"
          aria-label="Finish setup"
          :disabled="!canInitialize || consent === null || submitting"
          @click="finish"
        />
      </template>
      <template v-else>
        <UButton label="Back" color="neutral" variant="outline" aria-label="Back" :disabled="submitting" @click="backFromSources" />
        <UButton
          label="Finish setup"
          aria-label="Finish setup"
          :disabled="!canInitialize || submitting"
          @click="finish"
        />
      </template>
    </template>
  </GuiModal>
</template>
