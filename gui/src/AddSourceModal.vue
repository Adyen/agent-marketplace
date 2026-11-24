<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { RequestError, type SourceInput, type SourcePreview } from './api/client'
import type { WorkspaceController } from './controller'
import GuiModal from './GuiModal.vue'
import GuiIcon from './GuiIcon.vue'
import GuiSwitch from './GuiSwitch.vue'
import SourceForm from './SourceForm.vue'
import { describeSourceFailure, type SourceFailure } from './source-failure'
import { kindCounts, kindName } from './grouping'

const props = defineProps<{ controller: WorkspaceController }>()
const open = defineModel<boolean>('open', { default: false })

const formEpoch = ref(0)
const pending = ref(false)
const previewing = ref(false)
const draft = ref<SourceInput | null>(null)
const preview = ref<SourcePreview | null>(null)
const failures = ref<SourceFailure[]>([])
const picked = ref(new Set<string>())
let awaitingSettle = false
let previewAbort: AbortController | null = null

const serverLocationError = computed(() => {
  if (props.controller.state.sourceErrorField !== 'location') return undefined
  return props.controller.state.sourceError ?? undefined
})
const serverError = computed(() => {
  if (props.controller.state.sourceErrorField === 'location') return undefined
  return props.controller.state.sourceError ?? undefined
})
const valid = computed(() => preview.value?.resources.filter((resource) => resource.problem == null) ?? [])
const summary = computed(() => {
  const found = preview.value
  if (found == null) return ''
  const count = found.bundles.length > 0 ? found.bundles.length : valid.value.length
  const noun = found.bundles.length > 0 ? (count === 1 ? 'bundle' : 'bundles') : count === 1 ? 'resource' : 'resources'
  return `${count} ${noun}`
})

const pickedResources = computed(() => valid.value.filter((resource) => picked.value.has(resourceKey(resource))))
const allPicked = computed(() => valid.value.length > 0 && pickedResources.value.length === valid.value.length)
const confirmLabel = computed(() => {
  if (pending.value) return 'Adding…'
  const count = pickedResources.value.length
  return count === 0 ? 'Add source' : `Add source and install ${count}`
})

function resourceKey(resource: { kind: string; name: string }): string {
  return `${resource.kind}/${resource.name}`
}

function pick(resource: { kind: string; name: string }, on: boolean) {
  const next = new Set(picked.value)
  if (on) next.add(resourceKey(resource))
  else next.delete(resourceKey(resource))
  picked.value = next
}

function pickAll(on: boolean) {
  picked.value = new Set(on ? valid.value.map(resourceKey) : [])
}

let opener: HTMLElement | null = null

function clearErrors() {
  props.controller.state.sourceError = null
  props.controller.state.sourceErrorField = null
  failures.value = []
}

function stopPreview() {
  previewAbort?.abort()
  previewAbort = null
  previewing.value = false
}

function focusLocation() {
  void nextTick(() => document.querySelector<HTMLInputElement>('.gui-modal input[aria-label="Source location"]')?.focus())
}

watch(open, (isOpen) => {
  if (isOpen) opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
  stopPreview()
  preview.value = null
  draft.value = null
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
  clearErrors()
  awaitingSettle = false
  formEpoch.value += 1
  focusLocation()
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
  failures.value = errors.map((d) => describeSourceFailure(d.message))
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

async function add(source: SourceInput, install: { kind: string; name: string }[] = [], commit = '') {
  pending.value = true
  const accepted = await props.controller.addSource(source, install, commit)
  if (!accepted) {
    pending.value = false
    return
  }
  awaitingSettle = true
  if (settled()) finish()
}

async function submit(source: SourceInput) {
  if (source.kind === 'marketplace') return add(source)
  clearErrors()
  stopPreview()
  const abort = new AbortController()
  previewAbort = abort
  previewing.value = true
  try {
    const found = await props.controller.previewSource(source, abort.signal)
    if (abort.signal.aborted) return
    if (found.failure != null) {
      failures.value = [describeSourceFailure(found.failure, "The source couldn't be loaded.")]
      return
    }
    draft.value = source
    picked.value = new Set()
    preview.value = found
    void nextTick(() => document.querySelector<HTMLElement>('.add-source-modal__confirm')?.focus())
  } catch (error) {
    if (abort.signal.aborted) return
    const code = error instanceof RequestError ? error.code : ''
    if (code === 'localapi.directory_unavailable') {
      props.controller.state.sourceError = "This folder doesn't exist or isn't a directory."
      props.controller.state.sourceErrorField = 'location'
    } else {
      props.controller.state.sourceError = code === 'localapi.busy'
        ? 'Another action is running. Try again when it finishes.'
        : "The source couldn't be loaded."
    }
  } finally {
    if (previewAbort === abort) {
      previewAbort = null
      previewing.value = false
    }
  }
}

function back() {
  preview.value = null
  clearErrors()
  focusLocation()
}

function confirm() {
  const source = draft.value
  const found = preview.value
  if (source == null || found == null) return
  void add(
    source.kind === 'git' && found.ref !== '' ? { ...source, ref: found.ref } : source,
    pickedResources.value.map(({ kind, name }) => ({ kind, name })),
    found.commit,
  )
}
</script>

<template>
  <GuiModal
    v-model:open="open"
    title="Add source"
    icon="plus"
    :size="preview == null ? 'sm' : 'lg'"
    :busy="pending"
    :dismissible="!pending"
  >
    <div class="add-source-modal">
      <SourceForm
        v-show="preview == null"
        :key="formEpoch"
        :disabled="pending || previewing"
        :busy="pending || previewing"
        primary
        preview
        :server-location-error="serverLocationError"
        @add="submit"
        @kind="clearErrors"
        @edit-location="clearLocationError"
      >
        <template #actions>
          <UButton
            label="Cancel"
            class="gui-button--secondary"
            color="neutral"
            variant="soft"
            :disabled="pending"
            @click="open = false"
          />
        </template>
      </SourceForm>
      <section v-if="preview != null" class="add-source-modal__preview" aria-label="Source preview">
        <p class="add-source-modal__summary">
          <strong>{{ summary }}</strong>
          <template v-if="preview.commit !== ''">
            at <code class="mono" :title="preview.commit">{{ preview.commit.slice(0, 12) }}</code> on
            <code class="mono">{{ preview.ref }}</code>
          </template>
          <template v-else> in this folder</template>
          <span v-if="valid.length > 0 && preview.bundles.length === 0" class="add-source-modal__counts">
            {{ kindCounts(valid.map((resource) => resource.kind)) }}
          </span>
        </p>
        <p class="add-source-modal__location mono" :title="preview.location">{{ preview.location }}</p>
        <div v-if="valid.length > 0 && preview.bundles.length === 0" class="add-source-modal__pick-all">
          <span>Turn on the resources you want to install now. You can install the rest later.</span>
          <label class="add-source-modal__pick-all-control">
            Install all
            <GuiSwitch
              aria-label="Install all resources"
              :model-value="allPicked"
              :disabled="pending"
              @update:model-value="pickAll"
            />
          </label>
        </div>
        <ul v-if="preview.bundles.length > 0" class="add-source-modal__resources" aria-label="Bundles found">
          <li v-for="bundle in preview.bundles" :key="bundle" class="add-source-modal__resource">
            <span class="add-source-modal__kind">Bundle</span>
            <span class="add-source-modal__name mono">{{ bundle }}</span>
          </li>
        </ul>
        <ul v-else-if="preview.resources.length > 0" class="add-source-modal__resources" aria-label="Resources found">
          <li
            v-for="resource in preview.resources"
            :key="`${resource.kind}/${resource.name}`"
            class="add-source-modal__resource"
            :class="{ 'add-source-modal__resource--problem': resource.problem != null }"
          >
            <span class="add-source-modal__kind">{{ kindName(resource.kind) }}</span>
            <span class="add-source-modal__text">
              <span class="add-source-modal__name mono">{{ resource.name }}</span>
              <span class="add-source-modal__description">
                {{ resource.problem != null ? `Can't be read: ${resource.problem}` : resource.description }}
              </span>
            </span>
            <GuiSwitch
              v-if="resource.problem == null"
              :aria-label="`Install ${kindName(resource.kind)} ${resource.name}`"
              :model-value="picked.has(resourceKey(resource))"
              :disabled="pending"
              @update:model-value="pick(resource, $event)"
            />
          </li>
        </ul>
        <p v-else class="settings__notice">
          No resources were found here. Check that this is the folder that holds <code class="mono">skills/</code>,
          <code class="mono">commands/</code> or a plugin manifest.
        </p>
        <p v-for="warning in preview.warnings" :key="warning" class="settings__notice add-source-modal__warning">
          {{ warning }}
        </p>
        <div class="source-form__actions">
          <UButton
            label="Back"
            class="gui-button--secondary"
            color="neutral"
            variant="soft"
            :disabled="pending"
            @click="back"
          />
          <UButton
            :label="confirmLabel"
            :aria-label="pending ? 'Adding source' : confirmLabel"
            class="source-form__submit add-source-modal__confirm"
            :disabled="pending"
            @click="confirm"
          />
        </div>
      </section>
      <p v-if="serverError" role="alert" class="settings__notice settings__notice--danger">
        <GuiIcon name="error" class="settings__notice-icon" />
        <span>{{ serverError }}</span>
      </p>
      <div v-for="failure in failures" :key="failure.detail" role="alert" class="settings__notice settings__notice--danger">
        <GuiIcon name="error" class="settings__notice-icon" />
        <div class="add-source-modal__failure">
          <span>{{ failure.summary }}</span>
          <details>
            <summary>Details</summary>
            <p class="mono add-source-modal__detail">{{ failure.detail }}</p>
          </details>
        </div>
      </div>
    </div>
  </GuiModal>
</template>
