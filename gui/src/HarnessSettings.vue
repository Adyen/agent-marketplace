<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { Configuration } from './api/client'
import type { WorkspaceController } from './controller'
import SourceList from './SourceList.vue'

const props = defineProps<{ controller: WorkspaceController }>()
const open = defineModel<boolean>('open', { default: false })

const draft = ref<Set<string>>(new Set())
const loading = ref(false)
const actionError = ref<string | null>(null)
let openEpoch = 0

const configuration = computed<Configuration | null>(() => props.controller.state.configuration)

interface TargetOption {
  id: string
  supported: boolean
}

const options = computed<TargetOption[]>(() => {
  const config = configuration.value
  if (config == null) {
    return []
  }
  const supported = new Set(config.supportedTargets)
  const listed: TargetOption[] = config.supportedTargets.map((id) => ({ id, supported: true }))
  for (const id of config.userTargets) {
    if (!supported.has(id)) {
      listed.push({ id, supported: false })
    }
  }
  return listed
})

const busy = computed(() => props.controller.state.operation != null || props.controller.state.requesting)
const editable = computed(() => configuration.value?.editable === true)
const fieldsDisabled = computed(() => loading.value || busy.value || !editable.value)
const saveDisabled = computed(() => loading.value || !editable.value || draft.value.size === 0 || busy.value)

watch(open, () => {
  const epoch = ++openEpoch
  if (!open.value) {
    return
  }
  draft.value = new Set<string>()
  actionError.value = null
  loading.value = true
  void props.controller.loadConfiguration().then(() => {
    if (epoch !== openEpoch) {
      return
    }
    loading.value = false
    if (configuration.value == null) {
      actionError.value = 'Settings could not be loaded'
      return
    }
    draft.value = new Set(configuration.value.userTargets)
  })
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

function cancel() {
  draft.value = new Set<string>()
  open.value = false
}

async function save() {
  const order = options.value.map((option) => option.id)
  const targets = order.filter((id) => draft.value.has(id))
  for (const id of draft.value) {
    if (!order.includes(id)) {
      targets.push(id)
    }
  }
  if (await props.controller.saveTargets(targets)) {
    draft.value = new Set<string>()
    open.value = false
    return
  }
  actionError.value = props.controller.state.error ?? 'The action could not be started'
}
</script>

<template>
  <UModal v-model:open="open" title="Harness targets" aria-label="Harness targets">
    <template #content>
      <section class="settings" aria-label="Harness targets">
        <h2 class="settings__heading">Harness targets</h2>
        <p v-if="actionError" role="alert" class="settings__error">{{ actionError }}</p>
        <p v-if="configuration == null && !actionError" role="status">Loading settings…</p>
        <template v-if="configuration != null">
          <p v-if="!configuration.editable" role="status">The user configuration is not editable.</p>
          <fieldset class="settings__group" :disabled="fieldsDisabled">
            <legend>User targets</legend>
            <label
              v-for="option in options"
              :key="option.id"
              class="settings__option"
            >
              <UCheckbox
                :model-value="draft.has(option.id)"
                :label="option.id"
                :aria-label="`Target ${option.id}`"
                @update:model-value="toggle(option.id, $event)"
              />
              <span v-if="!option.supported" class="settings__badge">Unsupported by this version</span>
            </label>
          </fieldset>

          <section v-if="configuration.organizationTargets.length > 0" class="settings__group" aria-label="Organization targets">
            <h3>Organization targets</h3>
            <p>Managed by your organization; read-only.</p>
            <ul>
              <li v-for="id in configuration.organizationTargets" :key="id" class="mono">{{ id }}</li>
            </ul>
          </section>

          <SourceList title="User sources" :sources="configuration.userSources" :organization="false" />
          <SourceList title="Organization sources" :sources="configuration.organizationSources" :organization="true" />

          <section v-if="configuration.managedOutsideGUI.length > 0" class="settings__group" aria-label="Managed outside the GUI">
            <h3>Managed outside the GUI</h3>
            <ul>
              <li v-for="label in configuration.managedOutsideGUI" :key="label">{{ label }} — managed outside the GUI</li>
            </ul>
          </section>

        </template>
        <div class="settings__actions">
          <UButton label="Cancel" color="neutral" variant="outline" aria-label="Cancel" @click="cancel" />
          <UButton
            v-if="configuration != null"
            label="Save targets"
            aria-label="Save targets"
            :disabled="saveDisabled"
            @click="save"
          />
        </div>
      </section>
    </template>
  </UModal>
</template>
